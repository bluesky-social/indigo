#!/bin/bash
set -e
set -x 
./gosky --pds="http://localhost:4989" newAccount test@foo.com testman.pdstest password > test.auth
./gosky --pds="http://localhost:4989" --auth="test.auth" post "cats are really cool and the best"
./gosky --pds="http://localhost:4989" --auth="test.auth" post "paul frazee needs to buy a sweater"

