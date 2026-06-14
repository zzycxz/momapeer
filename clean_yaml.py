import os

path = r'c:\Users\13852\Desktop\Swarm-OS\momapeer\.goreleaser.yaml'
with open(path, 'rb') as f:
    data = f.read()

# Replace invalid utf-8 sequences
clean_data = data.decode('utf-8', errors='replace').replace('\ufffd', '-')
# Also just to be safe, I'll encode it back to standard utf-8
with open(path, 'w', encoding='utf-8') as f:
    f.write(clean_data)
print("Cleaned .goreleaser.yaml encoding.")
