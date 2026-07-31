import urllib.request, json, zipfile, io
# We can just fetch the log URL for the failed jobs if possible, or explain to the user.
# The GitHub API requires authentication to download logs.
print("Cannot download logs unauthenticated via API, but we know which jobs failed.")
