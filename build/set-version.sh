#!/usr/bin/env bash
# Stamp a version (e.g. 0.2.0) into every build asset that carries one. Used by the release workflow.
set -euo pipefail
v="${1:?usage: set-version.sh <version>}"
cd "$(dirname "$0")/.."
sed -i.bak -E "s/^(  version: )\"[^\"]*\"/\1\"$v\"/" build/config.yml
sed -i.bak -E "s/^(version: )\"[^\"]*\"/\1\"$v\"/" build/linux/nfpm/nfpm.yaml
for f in build/darwin/Info.plist build/darwin/Info.dev.plist; do
  perl -0pi -e "s|(<key>CFBundle(Short)?Version(String)?</key>\s*<string>)[^<]*|\${1}$v|g" "$f"
done
rm -f build/config.yml.bak build/linux/nfpm/nfpm.yaml.bak
echo "version set to $v"
