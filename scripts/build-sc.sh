#!/bin/bash

BRANCH=main
NEXUS_BRANCH=main

# build Apex-bridge smartcontracts
cd ./apex-bridge-smartcontracts
git checkout main
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

# build Nexus-bridge smartcontracts
cd ./apex-evm-gateway
git checkout main
git fetch origin
git pull origin
if [ "$NEXUS_BRANCH" != "main" ]; then
    echo "SWITCHING TO ${NEXUS_BRANCH}"
    git branch -D ${NEXUS_BRANCH}
    git switch ${NEXUS_BRANCH}
    git pull origin # this is not important but lets have it here
fi
npm i && npx hardhat compile
cd ..

go run consensus/polybft/contractsapi/apex-artifacts-gen/main.go
go run consensus/polybft/contractsapi/nexus-artifacts-gen/main.go
go run consensus/polybft/contractsapi/bindings-gen/main.go
./scripts/buildb.sh