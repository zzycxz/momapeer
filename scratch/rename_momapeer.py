import os
import glob
import re

base_dir = r"c:\Users\13852\Desktop\Swarm-OS\MoMAPeer"

# 1. Rename cmd/MoMAPeer -> cmd/momapeer
cmd_old = os.path.join(base_dir, "cmd", "MoMAPeer")
cmd_new = os.path.join(base_dir, "cmd", "momapeer")
if os.path.exists(cmd_old):
    os.rename(cmd_old, cmd_new)
    print(f"Renamed {cmd_old} to {cmd_new}")

# 2. Content replacements
files_to_check = [
    "Makefile",
    ".goreleaser.yaml",
    ".goreleaser.yml",
    "internal/config/config.go",
    "internal/cli/setup.go",
    "internal/cli/cli.go",
    "internal/cli/chat_tui.go",
    "internal/cli/statusline.go",
    "internal/control/context.go",
    "internal/agent/memory.go",
]

# Find all go files just in case
all_go_files = glob.glob(os.path.join(base_dir, "**", "*.go"), recursive=True)
all_yaml_files = glob.glob(os.path.join(base_dir, "**", "*.yaml"), recursive=True)
all_yml_files = glob.glob(os.path.join(base_dir, "**", "*.yml"), recursive=True)
all_makefiles = glob.glob(os.path.join(base_dir, "Makefile"), recursive=True)

targets = list(set(all_go_files + all_yaml_files + all_yml_files + all_makefiles))

count = 0
for f in targets:
    if not os.path.exists(f):
        continue
    try:
        with open(f, "r", encoding="utf-8") as file:
            content = file.read()
            
        # We want to replace:
        # - "MoMAPeer.toml" -> "momapeer.toml"
        # - "MOMAPEER.md" -> "momapeer.md" (if memory file)
        # - ".MoMAPeer/" -> ".momapeer/"
        # - "MoMAPeer" when it refers to binary, e.g., in Makefile or .goreleaser
        
        original = content
        
        # Specific exact replacements that are safe in code:
        content = content.replace("MoMAPeer.toml", "momapeer.toml")
        content = content.replace("MOMAPEER.md", "momapeer.md")
        content = content.replace(".MoMAPeer", ".momapeer")
        content = content.replace("cmd/MoMAPeer", "cmd/momapeer")
        
        # In Makefile and .goreleaser, we might have generic MoMAPeer as binary name
        if "Makefile" in f or ".goreleaser" in f:
            content = content.replace("MoMAPeer", "momapeer")
            
        if content != original:
            with open(f, "w", encoding="utf-8") as file:
                file.write(content)
            count += 1
            print(f"Updated content in {f}")
    except Exception as e:
        print(f"Error processing {f}: {e}")

print(f"Total files updated: {count}")
