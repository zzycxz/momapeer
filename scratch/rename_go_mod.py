import os
import glob
import re

base_dir = r"c:\Users\13852\Desktop\Swarm-OS\MoMAPeer"

old_import = "github.com/zzycxz/MoMAPeer"
new_import = "github.com/zzycxz/momapeer"

# Find all go files, mod files, workflows, markdowns, etc.
all_go_files = glob.glob(os.path.join(base_dir, "**", "*.go"), recursive=True)
all_mod_files = glob.glob(os.path.join(base_dir, "**", "go.mod"), recursive=True)
all_md_files = glob.glob(os.path.join(base_dir, "**", "*.md"), recursive=True)
all_yaml_files = glob.glob(os.path.join(base_dir, "**", "*.yaml"), recursive=True)
all_yml_files = glob.glob(os.path.join(base_dir, "**", "*.yml"), recursive=True)
all_makefiles = glob.glob(os.path.join(base_dir, "Makefile"), recursive=True)
all_js_ts = glob.glob(os.path.join(base_dir, "**", "*.ts"), recursive=True) + glob.glob(os.path.join(base_dir, "**", "*.js"), recursive=True) + glob.glob(os.path.join(base_dir, "**", "*.svelte"), recursive=True)

targets = list(set(all_go_files + all_mod_files + all_md_files + all_yaml_files + all_yml_files + all_makefiles + all_js_ts))

count = 0
for f in targets:
    if not os.path.exists(f):
        continue
    try:
        with open(f, "r", encoding="utf-8") as file:
            content = file.read()
            
        original = content
        
        # Replace module path
        content = content.replace(old_import, new_import)
        
        if content != original:
            with open(f, "w", encoding="utf-8") as file:
                file.write(content)
            count += 1
            print(f"Updated imports in {f}")
    except Exception as e:
        print(f"Error processing {f}: {e}")

print(f"Total files updated: {count}")
