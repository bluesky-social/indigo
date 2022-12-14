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


