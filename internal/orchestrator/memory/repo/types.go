// Package repo hosts the concrete Postgres implementations for every
// MemPalace persistence sub-package (drawerrepo, kgrepo, centroidrepo,
// palacegraphrepo, identityrepo). The repository **contracts** are no
// longer declared here — per project convention (consumer-side
// interfaces), each consumer package declares its own narrow interface
// over the concrete repo it holds. This file is now intentionally
// interface-free.
package repo
