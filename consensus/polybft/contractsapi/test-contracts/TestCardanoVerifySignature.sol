// SPDX-License-Identifier: MIT
// TestCardanoVerifySignature.sol
// Contract which testst ValidatorSet precompile
pragma solidity ^0.8.0;

contract TestCardanoVerifySignature {
    address constant PRECOMPILE = 0x0000000000000000000000000000000000002050;
    uint256 constant PRECOMPILE_GAS = 150000;

    function check(bytes calldata txRaw, bytes calldata signature, bytes32 verifyingKey) public view returns (bool) {
        (bool callSuccess, bytes memory returnData) = PRECOMPILE.staticcall{gas: PRECOMPILE_GAS}(
            abi.encode(txRaw, signature, verifyingKey, true)
        );
    
        return callSuccess && abi.decode(returnData, (bool));
    }

    function checkMsg(bytes calldata keyHash, bytes calldata signature, bytes32 verifyingKey) public view returns (bool) {
       (bool callSuccess, bytes memory returnData) = PRECOMPILE.staticcall{
            gas: PRECOMPILE_GAS
        }(abi.encode(string(abi.encodePacked("hello world: ", keyHash)), signature, verifyingKey, false));
    
        return callSuccess && abi.decode(returnData, (bool));
    }
}
