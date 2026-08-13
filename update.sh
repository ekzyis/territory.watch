#!/usr/bin/env bash
#
# This DELETES all existing territory data before fetching it again. It then deploys the new data
# with rsync.
#
# TODO: For now, I run this manually with sudo, but ideally, this should run as the nginx user and a
# systemd service + timer.

set -xeuo pipefail

start="$(date +%s)"

# delete existing data so the next fetch will work (it resumes by default)
rm static/data/* || true

# fetch new data
time make build-all

# update archive
archive=data.tar.gz
rm $archive
tar zcvf $archive static/

# commit new data
git add $archive
export GIT_AUTHOR_NAME="systemd"
export GIT_AUTHOR_EMAIL="noreply@territory.watch"
export GIT_COMMITTER_NAME="systemd"
export GIT_COMMITTER_EMAIL="noreply@territory.watch"
end=$(date +%s)
took=$((end-start))
git commit -n --allow-empty \
  -m $'systemd: automated update\n\n' \
  -m "took $took seconds"

# deploy new data
#
# NOTE: The last / in ./static/ is important to replace the target directory, instead of
# moving the source directory into it.
rsync -r --delete \
  --chown=nginx:nginx --chmod=D755,F644 \
  ./static/ /var/www/territory.watch/
