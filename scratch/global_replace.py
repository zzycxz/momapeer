import os
import glob

base_dir = r"c:\Users\13852\Desktop\Swarm-OS\MoMAPeer"

# We want to replace "MoMAPeer" with "momapeer" globally
old_str = "MoMAPeer"
new_str = "momapeer"

# Collect all relevant files
exts = ["*.go", "*.md", "*.yaml", "*.yml", "Makefile", "*.toml", "go.mod", "*.ts", "*.js", "*.json", "*.html", "*.css", "*.ps1", "*.sh"]
targets = []
for ext in exts:
    targets.extend(glob.glob(os.path.join(base_dir, "**", ext), recursive=True))

# Remove go.sum from targets if it got in, we will handle it with go mod tidy
targets = [t for t in targets if not t.endswith("go.sum") and "node_modules" not in t and ".git" not in t]

count = 0
for f in set(targets):
    if not os.path.isfile(f):
        continue
    try:
        with open(f, "r", encoding="utf-8") as file:
            content = file.read()
            
        original = content
        content = content.replace(old_str, new_str)
        
        if content != original:
            with open(f, "w", encoding="utf-8") as file:
                file.write(content)
            count += 1
            print(f"Updated {f}")
    except Exception as e:
        pass

print(f"Total files updated: {count}")
