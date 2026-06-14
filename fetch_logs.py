import urllib.request, json
url = 'https://api.github.com/repos/zzycxz/momapeer/actions/runs?per_page=3'
req = urllib.request.Request(url, headers={'User-Agent': 'Mozilla/5.0'})
try:
    with urllib.request.urlopen(req) as response:
        data = json.loads(response.read())
        for run in data['workflow_runs']:
            run_id = run['id']
            jobs_url = run['jobs_url']
            print(f'Run ID: {run_id}')
            print(f'Status: {run["conclusion"]}')
            
            req2 = urllib.request.Request(jobs_url, headers={'User-Agent': 'Mozilla/5.0'})
            with urllib.request.urlopen(req2) as res2:
                jdata = json.loads(res2.read())
                for j in jdata['jobs']:
                    if j['conclusion'] == 'failure':
                        print(f"  Job {j['name']} failed.")
                        for step in j['steps']:
                            if step['conclusion'] == 'failure':
                                print(f"    Failed step: {step['name']}")
except Exception as e:
    print('Error:', e)
