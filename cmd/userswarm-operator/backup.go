// Backup subcommand — tars workspace files from a PVC and uploads to S3.
// Runs inside a K8s Job with the PVC mounted read-only at --workspace.
// Same binary as the operator, just a different entrypoint.
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

// Max file size to include in backup (50MB). Anything larger is skipped.
const maxBackupFileSize = 50 * 1024 * 1024

// File extensions worth backing up. Everything else is temp/cache/disposable.
var backupExtensions = map[string]bool{
	".db":     true, // SQLite databases (sessions, memory, cron)
	".db-wal": true, // SQLite WAL files (needed for consistency)
	".db-shm": true, // SQLite shared memory
	".md":     true, // SOUL.md, IDENTITY.md, memory markdown
	".json":   true, // State files, config
}

func newBackupCommand() *cobra.Command {
	var (
		workspace string
		bucket    string
		region    string
		prefix    string // "hourly" or "final"
		userID    string
		swarmName string
		env       string
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup workspace files to S3",
		Long:  "Tar selected workspace files and upload to S3. Runs as a Job with PVC mounted read-only.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if bucket == "" || workspace == "" {
				return fmt.Errorf("--bucket and --workspace are required")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 4*time.Minute)
			defer cancel()

			return runBackup(ctx, backupOpts{
				workspace: workspace,
				bucket:    bucket,
				region:    region,
				prefix:    prefix,
				userID:    userID,
				swarmName: swarmName,
				env:       env,
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "/zeroclaw-data/workspace", "Path to the ZeroClaw workspace directory")
	cmd.Flags().StringVar(&bucket, "bucket", os.Getenv("BACKUP_BUCKET"), "S3 bucket name")
	cmd.Flags().StringVar(&region, "region", os.Getenv("AWS_DEFAULT_REGION"), "AWS region")
	cmd.Flags().StringVar(&prefix, "prefix", "hourly", "S3 path prefix (hourly or final)")
	cmd.Flags().StringVar(&userID, "user-id", os.Getenv("USER_ID"), "User ID for S3 path")
	cmd.Flags().StringVar(&swarmName, "swarm-name", os.Getenv("SWARM_NAME"), "Swarm name for S3 path")
	cmd.Flags().StringVar(&env, "env", os.Getenv("ENV"), "Environment name for S3 path")

	return cmd
}

type backupOpts struct {
	workspace string
	bucket    string
	region    string
	prefix    string
	userID    string
	swarmName string
	env       string
}

func runBackup(ctx context.Context, opts backupOpts) error {
	// Check workspace exists
	if _, err := os.Stat(opts.workspace); err != nil {
		fmt.Println("No workspace directory found, skipping backup")
		return nil
	}

	// Collect files to backup
	var files []string
	err := filepath.Walk(opts.workspace, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable files
		}
		if info.IsDir() {
			return nil
		}
		if info.Size() > maxBackupFileSize {
			return nil
		}
		ext := filepath.Ext(info.Name())
		// Handle .db-wal and .db-shm (double extension)
		if !backupExtensions[ext] {
			base := strings.TrimSuffix(info.Name(), ext)
			ext2 := filepath.Ext(base) + ext
			if !backupExtensions[ext2] {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking workspace: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("No workspace files found, skipping backup")
		return nil
	}

	fmt.Printf("Backing up %d files\n", len(files))
	for _, f := range files {
		rel, _ := filepath.Rel(filepath.Dir(opts.workspace), f)
		fmt.Printf("  %s\n", rel)
	}

	// Create tar.gz archive in a temp file
	tmpFile, err := os.CreateTemp("", "backup-*.tar.gz")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if err := createArchive(tmpFile, opts.workspace, files); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("creating archive: %w", err)
	}
	_ = tmpFile.Close()

	// Print archive size
	if info, err := os.Stat(tmpFile.Name()); err == nil {
		fmt.Printf("Archive size: %d bytes\n", info.Size())
	}

	// Upload to S3
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	key := fmt.Sprintf("%s/swarms/%s/%s/%s/%s.tar.gz",
		opts.env, opts.userID, opts.swarmName, opts.prefix, timestamp)

	if err := uploadToS3(ctx, opts.bucket, opts.region, key, tmpFile.Name()); err != nil {
		return fmt.Errorf("uploading to S3: %w", err)
	}

	fmt.Printf("Backup uploaded to s3://%s/%s\n", opts.bucket, key)
	return nil
}

// createArchive writes selected files into a tar.gz archive.
func createArchive(w io.Writer, baseDir string, files []string) error {
	gw := gzip.NewWriter(w)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	for _, path := range files {
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			continue
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing header for %s: %w", rel, err)
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := io.Copy(tw, f); err != nil {
			_ = f.Close()
			return fmt.Errorf("writing %s: %w", rel, err)
		}
		_ = f.Close()
	}

	return nil
}

// uploadToS3 uploads a file to an S3 bucket.
func uploadToS3(ctx context.Context, bucket, region, key, filePath string) error {
	// Credentials from env vars (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set")
	}

	client := s3.New(s3.Options{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	})

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   f,
	})
	return err
}
