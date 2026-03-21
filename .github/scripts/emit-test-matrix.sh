#!/usr/bin/env bash
# Emit JSON for the CI test job matrix: test-all (all packages except ./itest/...)
# plus one parallel job per itest/ittestgroup* directory.
set -euo pipefail
cd "${GITHUB_WORKSPACE}"

python3 <<'PY'
import json
import os
from pathlib import Path

root = Path(os.environ["GITHUB_WORKSPACE"])
items = [{"name": "test-all", "kind": "pkg-list"}]
for p in sorted(root.glob("itest/ittestgroup*")):
    if p.is_dir():
        items.append(
            {"name": f"itest-{p.name}", "kind": "dir", "target": f"./itest/{p.name}"}
        )

out = os.environ["GITHUB_OUTPUT"]
with open(out, "a", encoding="utf-8") as f:
    f.write("matrix<<EOF\n")
    f.write(json.dumps(items))
    f.write("\nEOF\n")
PY
