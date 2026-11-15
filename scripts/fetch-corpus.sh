#!/usr/bin/env sh
# Descarga corpus de configuraciones reales para los tests de no-regresión.
# Se guardan en core/testdata/corpus/ (ignorado por git) y los usa
# TestCorpus en core/corpus_test.go si existen.
#
#   HariSekhon/HAProxy-configs — MIT — https://github.com/HariSekhon/HAProxy-configs
set -eu
dest="$(cd "$(dirname "$0")/.." && pwd)/core/testdata/corpus"
mkdir -p "$dest"
if [ -d "$dest/HariSekhon/.git" ]; then
  git -C "$dest/HariSekhon" pull --ff-only --quiet
else
  git clone --depth 1 --quiet https://github.com/HariSekhon/HAProxy-configs.git "$dest/HariSekhon"
fi
echo "Corpus listo en $dest. Ejecuta: go test ./core -run TestCorpus -v"
