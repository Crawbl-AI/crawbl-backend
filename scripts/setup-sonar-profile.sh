#!/usr/bin/env bash
set -euo pipefail

# Creates and configures the "Crawbl Go" quality profile on SonarCloud.
# Idempotent: safe to re-run (overwrites existing rule settings).
#
# Usage:
#   SONARQUBE_TOKEN=<token> ./scripts/setup-sonar-profile.sh
#
# The profile copies "Sonar way" and tightens rules to match the project's
# CLAUDE.md conventions (4 params, 60-line functions, 3-level nesting, etc.).

SONAR_URL="${SONARQUBE_URL:-https://sonarcloud.io}"
SONAR_TOKEN="${SONARQUBE_TOKEN:?SONARQUBE_TOKEN is required}"
ORG="crawbl-ai"
PROJECT="Crawbl-AI_crawbl-backend"
PROFILE_NAME="Crawbl Go"
LANGUAGE="go"

echo "==> Checking for existing '${PROFILE_NAME}' profile..."
existing=$(curl -s -u "${SONAR_TOKEN}:" \
  "${SONAR_URL}/api/qualityprofiles/search?language=${LANGUAGE}&organization=${ORG}" \
  | python3 -c "
import json,sys
data=json.load(sys.stdin)
for p in data.get('profiles',[]):
    if p['name']=='${PROFILE_NAME}':
        print(p['key'])
        break
" 2>/dev/null || true)

if [ -n "${existing}" ]; then
  echo "    Profile already exists (key: ${existing})"
  PROFILE_KEY="${existing}"
else
  echo "==> Copying 'Sonar way' to '${PROFILE_NAME}'..."
  sonar_way_key=$(curl -s -u "${SONAR_TOKEN}:" \
    "${SONAR_URL}/api/qualityprofiles/search?language=${LANGUAGE}&organization=${ORG}" \
    | python3 -c "
import json,sys
data=json.load(sys.stdin)
for p in data.get('profiles',[]):
    if p['name']=='Sonar way':
        print(p['key'])
        break
")
  PROFILE_KEY=$(curl -s -u "${SONAR_TOKEN}:" \
    "${SONAR_URL}/api/qualityprofiles/copy" \
    -d "fromKey=${sonar_way_key}&toName=${PROFILE_NAME}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['key'])")
  echo "    Created profile (key: ${PROFILE_KEY})"
fi

echo "==> Configuring rules..."

activate_rule() {
  local rule="$1"
  local params="${2:-}"
  local data="key=${PROFILE_KEY}&rule=go:${rule}"
  if [ -n "${params}" ]; then
    data="${data}&params=${params}"
  fi
  local result
  result=$(curl -s -u "${SONAR_TOKEN}:" "${SONAR_URL}/api/qualityprofiles/activate_rule" -d "${data}" 2>&1)
  if [ -n "${result}" ] && echo "${result}" | python3 -c "import json,sys; d=json.load(sys.stdin); sys.exit(0 if 'errors' in d else 1)" 2>/dev/null; then
    echo "    WARN: ${rule}: ${result}"
  else
    echo "    OK: ${rule} ${params}"
  fi
}

# Modified rules (already in Sonar way, tightened thresholds).
activate_rule "S3776" "threshold=12"          # Cognitive complexity (was 15)
activate_rule "S107"  "max=4"                 # Max function params (was 7)

# Newly activated rules.
activate_rule "S103"  "maximumLineLength=110" # Line length
activate_rule "S104"  "max=400"               # File length
activate_rule "S1067"                         # Expression complexity
activate_rule "S126"                          # if/else must end with else
activate_rule "S131"                          # switch must have default
activate_rule "S134"  "max=3"                 # Nesting depth
activate_rule "S138"  "max=60"                # Function length
activate_rule "S1145"                         # Useless if(true)
activate_rule "S1151" "max=15"                # Switch case lines

echo "==> Associating profile with project..."
result=$(curl -s -u "${SONAR_TOKEN}:" \
  "${SONAR_URL}/api/qualityprofiles/add_project" \
  -d "key=${PROFILE_KEY}&project=${PROJECT}" 2>&1)
if [ -n "${result}" ]; then
  echo "    NOTE: Could not auto-associate (may need admin). Set via UI:"
  echo "    ${SONAR_URL}/project/quality_profiles?id=${PROJECT}"
else
  echo "    OK: Profile linked to ${PROJECT}"
fi

echo "==> Verifying profile..."
curl -s -u "${SONAR_TOKEN}:" \
  "${SONAR_URL}/api/qualityprofiles/search?language=${LANGUAGE}&organization=${ORG}" \
  | python3 -c "
import json,sys
data=json.load(sys.stdin)
for p in data.get('profiles',[]):
    if p['name']=='${PROFILE_NAME}':
        print(f\"  Name: {p['name']}\")
        print(f\"  Key: {p['key']}\")
        print(f\"  Active rules: {p['activeRuleCount']}\")
        print(f\"  Default: {p['isDefault']}\")
        break
"

echo ""
echo "Done. If the profile is not set as default, set it manually:"
echo "  ${SONAR_URL}/organizations/${ORG}/quality_profiles/show?language=${LANGUAGE}&name=${PROFILE_NAME// /+}"
