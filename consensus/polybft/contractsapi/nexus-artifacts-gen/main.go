package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"log"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/common"
)

const (
	extension = ".sol"
)

func main() {
	_, filename, _, _ := runtime.Caller(0) //nolint: dogsled
	currentPath := path.Dir(filename)
	proxyscpath := path.Join(currentPath, "../../../../apex-evm-gateway/artifacts/@openzeppelin/contracts/proxy/")
	nexusscpath := path.Join(currentPath, "../../../../apex-evm-gateway/artifacts/contracts/")

	str := `// This is auto-generated file. DO NOT EDIT.
package contractsapi

`

	proxyContracts := []struct {
		Path string
		Name string
	}{
		{
			"ERC1967/ERC1967Proxy.sol",
			"ERC1967Proxy",
		},
	}

	nexusContracts := []struct {
		Path string
		Name string
	}{
		{
			"ERC20TokenPredicate.sol",
			"Nexus_ERC20TokenPredicate",
		},
		{
			"Gateway.sol",
			"Nexus_Gateway",
		},
		{
			"NativeERC20Mintable.sol",
			"Nexus_NativeERC20Mintable",
		},
		{
			"Validators.sol",
			"Nexus_Validators",
		},
	}

	for _, v := range proxyContracts {
		artifactBytes, err := contracts.ReadRawArtifact(proxyscpath, v.Path, getContractName(v.Path))
		if err != nil {
			log.Fatal(err)
		}

		dst := &bytes.Buffer{}
		if err = json.Compact(dst, artifactBytes); err != nil {
			log.Fatal(err)
		}

		str += fmt.Sprintf("var %sArtifact string = `%s`\n", v.Name, dst.String())
	}

	for _, v := range nexusContracts {
		artifactBytes, err := contracts.ReadRawArtifact(nexusscpath, v.Path, getContractName(v.Path))
		if err != nil {
			log.Fatal(err)
		}

		dst := &bytes.Buffer{}
		if err = json.Compact(dst, artifactBytes); err != nil {
			log.Fatal(err)
		}

		str += fmt.Sprintf("var %sArtifact string = `%s`\n", v.Name, dst.String())
	}

	output, err := format.Source([]byte(str))
	if err != nil {
		fmt.Println(str)
		log.Fatal(err)
	}

	if err = common.SaveFileSafe(path.Join(path.Dir(path.Clean(currentPath)),
		"nexus_sc_data.go"), output, 0600); err != nil {
		log.Fatal(err)
	}
}

// getContractName extracts smart contract name from provided path
func getContractName(path string) string {
	pathSegments := strings.Split(path, string([]rune{os.PathSeparator}))
	nameSegment := pathSegments[len(pathSegments)-1]

	return strings.Split(nameSegment, extension)[0]
}
