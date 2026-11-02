#!/bin/sh
# Re-fetches the third-party micro plugins bundled into deck at their pinned commits.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
plug="$root/internal/editor/runtime/plug"

fetch() {
	name=$1 repo=$2 sha=$3 prefix=$4
	shift 4
	dest="$plug/$name"
	mkdir -p "$dest"
	for f in "$@"; do
		mkdir -p "$dest/$(dirname "$f")"
		curl -fsSL "https://raw.githubusercontent.com/$repo/$sha/$prefix$f" -o "$dest/$f"
	done
	echo "$name: $repo@$sha"
}

fetch mlsp Andriamanitra/mlsp 91261a0926c9e95d059cf5854a0c2e8d4e7d4051 "" \
	main.lua json.lua config.lua LICENSE

fetch filemanager micro-editor/updated-plugins 216ec3adaf3adec78665614402ece56cf60ae713 filemanager-plugin/ \
	filemanager.lua syntax.yaml repo.json LICENSE

fetch MicroOmni Neko-Box-Coder/MicroOmni f770333935cf47318300309ffc0b2867a190e48e "" \
	MicroOmni.lua Common.lua Diff.lua Highlight.lua History.lua Minimap.lua Search.lua Session.lua WordJump.lua \
	repo.json LICENSE

fetch editorconfig 10sr/editorconfig-micro 892aa42b50e6f57a5065b6f4df8c4e0a3913c9ee "" \
	editorconfig.lua repo.json LICENSE help/editorconfig.md syntax/editorconfig.yaml
