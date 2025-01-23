#!/bin/bash

BRANCH=main

# build Apex-bridge smartcontracts
cd ./apex-bridge-smartcontracts
git checkout audit/apex-451
git fetch origin
git pull origin
if [ "$BRANCH" != "main" ]; then
    echo "SWITCHING TO ${BRANCH}"
    git branch -D ${BRANCH}
    git switch ${BRANCH}
    git pull origin # this is not important but lets have it here
fi
npm i && npx hardhat compile
cd ..

go run consensus/polybft/contractsapi/apex-artifacts-gen/main.go
go run consensus/polybft/contractsapi/bindings-gen/main.go
./scripts/buildb.sh