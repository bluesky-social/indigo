#!/bin/bash
set -e
set -x 


echo "1. Creating Account"
./gosky --pds="http://localhost:4989" newAccount test@foo.com testman.pdstest password > test.auth

echo "2. Some Content"
./gosky --pds="http://localhost:4989" --auth="test.auth" post "cats are really cool and the best"
./gosky --pds="http://localhost:4989" --auth="test.auth" post "paul frazee needs to buy a sweater"

echo "3. View That Content"
./gosky --pds="http://localhost:4989" --auth="test.auth" getAuthorFeed


echo "4. Make a second account"
./gosky --pds="http://localhost:4989" newAccount test2@foo.com friendbot.pdstest password > test2.auth

echo "5. Post on second account"
./gosky --pds="http://localhost:4989" --auth="test2.auth" post "Im a big fan of the snow"

echo "6. Upvote content"
posturi=$(./gosky --pds=http://localhost:4989 --auth=test.auth getAuthorFeed | jq -r .uri | head -n1)
./gosky --pds="http://localhost:4989" --auth="test2.auth" feedSetVote $posturi up

echo "7. Check notifications"
./gosky --pds="http://localhost:4989" --auth="test.auth" notifs

echo "8. Follow"
./gosky --pds="http://localhost:4989" --auth="test2.auth" follows add $(cat test.auth | jq -r .did)

echo "9. Check notifications"
./gosky --pds="http://localhost:4989" --auth="test.auth" notifs
