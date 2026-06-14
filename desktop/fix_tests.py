import os

for f in os.listdir('.'):
    if f.endswith('_test.go'):
        with open(f, 'r', encoding='utf-8') as file:
            content = file.read()
        new_content = content.replace('"MoMA"', '"moma"').replace('MoMA/', 'moma/').replace('MoMA-token-plan', 'moma-token-plan')
        if content != new_content:
            with open(f, 'w', encoding='utf-8') as file:
                file.write(new_content)
            print(f'Updated {f}')
