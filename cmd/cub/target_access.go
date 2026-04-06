// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/hkdf"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var targetAccessArgs struct {
	ttl    string
	output string
}

var targetAccessCmd = &cobra.Command{
	Use:   "access <target-slug> <access-config-unit-slug>",
	Short: "Get temporary kubectl access to a cluster",
	Long: getCommandHelp(`
Request temporary kubectl access by invoking the generate-kubecontext
function on an access policy unit applied to a target. The unit must
contain a ServiceAccount with the confighub.com/generate-kubecontext: allow
annotation and must have been applied to the target.

The resulting kubeconfig is encrypted end-to-end — the ConfigHub server
never sees the credentials.`, `
  # Print kubeconfig to stdout
  cub target access --space infra my-cluster access-readonly

  # Save to a file and use with kubectl
  cub target access --space infra my-cluster access-readonly --output ~/.kube/confighub-prod
  kubectl --kubeconfig ~/.kube/confighub-prod get pods

  # Pipe directly
  cub target access --space infra my-cluster access-readonly | kubectl --kubeconfig /dev/stdin get pods

  # Request a shorter TTL
  cub target access --space infra my-cluster access-readonly --ttl 30m`),
	Args:    cobra.ExactArgs(2),
	PreRunE: spacePreRunE,
	RunE:    targetAccessRun,
}

func init() {
	targetAccessCmd.Flags().StringVar(&targetAccessArgs.ttl, "ttl", "", "requested token TTL (e.g., 30m, 1h); capped by the policy's max-ttl")
	targetAccessCmd.Flags().StringVar(&targetAccessArgs.output, "output", "", "write kubeconfig to this file instead of stdout")
	targetCmd.AddCommand(targetAccessCmd)
}

func targetAccessRun(cmd *cobra.Command, args []string) error {
	targetSlug := args[0]
	unitSlug := args[1]

	// Look up the target.
	target, err := apiGetTargetFromSlugInSpaceCore(targetSlug, selectedSpaceID, "TargetID,BridgeWorkerID")
	if err != nil {
		return fmt.Errorf("target %q not found in space", targetSlug)
	}

	// Look up the unit.
	unit, err := apiGetUnitFromSlug(unitSlug, "UnitID,TargetID,LastAppliedRevisionNum")
	if err != nil {
		return fmt.Errorf("unit %q not found in space", unitSlug)
	}

	// Check the unit is associated with this target.
	if unit.TargetID == nil || *unit.TargetID != uuid.UUID(target.TargetID) {
		return fmt.Errorf("unit %q is not associated with target %q; use 'cub unit set-target' first", unitSlug, targetSlug)
	}

	// Check the unit has been applied.
	if unit.LastAppliedRevisionNum == 0 {
		return fmt.Errorf("unit %q has not been applied yet; run 'cub unit apply --space %s %s' first", unitSlug, selectedSpaceID, unitSlug)
	}

	// Generate ephemeral X25519 keypair.
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return errors.Wrap(err, "failed to generate keypair")
	}
	pubKeyB64 := base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes())

	// Build the function invocation request.
	functionName := "generate-kubecontext"
	funcArgs := []goclientnew.FunctionArgument{
		newFuncArg("public-key", pubKeyB64),
	}
	if targetAccessArgs.ttl != "" {
		funcArgs = append(funcArgs, newFuncArg("ttl", targetAccessArgs.ttl))
	}

	invocations := []goclientnew.FunctionInvocation{
		{
			FunctionName: functionName,
			Arguments:    funcArgs,
		},
	}

	workerID := goclientnew.UUID(target.BridgeWorkerID)
	whereClause := fmt.Sprintf("Slug = '%s'", unitSlug)
	req := goclientnew.FunctionInvocationsRequest{
		ToolchainType:       "Kubernetes/YAML",
		BridgeWorkerID:      &workerID,
		FunctionInvocations: &invocations,
	}

	params := &goclientnew.InvokeFunctionsParams{
		Where: &whereClause,
	}

	// Invoke the function.
	funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), params, req)
	if cubapi.IsAPIError(err, funcRes) {
		return errors.Wrap(cubapi.InterpretErrorGeneric(err, funcRes), "failed to invoke generate-kubecontext")
	}

	var responses []goclientnew.FunctionInvocationsResponse
	if funcRes.JSON200 != nil {
		responses = *funcRes.JSON200
	} else if funcRes.JSON207 != nil {
		responses = *funcRes.JSON207
	}

	if len(responses) == 0 {
		return fmt.Errorf("no response from generate-kubecontext function")
	}

	resp := responses[0]
	if resp.Error != nil {
		return fmt.Errorf("function error: %s", resp.Error.Message)
	}

	// Extract the encrypted output.
	opaqueOutput, ok := resp.Outputs["Opaque"]
	if !ok || len(opaqueOutput) == 0 {
		return fmt.Errorf("no encrypted output returned from function")
	}

	encryptedJSON, err := base64.StdEncoding.DecodeString(opaqueOutput)
	if err != nil {
		return errors.Wrap(err, "failed to decode function output")
	}

	// Decrypt the kubeconfig.
	kubeconfig, err := decryptAccessResponse(privateKey, encryptedJSON)
	if err != nil {
		return errors.Wrap(err, "failed to decrypt kubeconfig")
	}

	if targetAccessArgs.output != "" {
		// Write to file, messages to stderr.
		dir := filepath.Dir(targetAccessArgs.output)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return errors.Wrapf(err, "failed to create directory %s", dir)
		}
		if err := os.WriteFile(targetAccessArgs.output, kubeconfig, 0600); err != nil {
			return errors.Wrapf(err, "failed to write kubeconfig to %s", targetAccessArgs.output)
		}
		fmt.Fprintf(os.Stderr, "Kubeconfig written to %s\n", targetAccessArgs.output)
	} else {
		// Print to stdout for piping.
		fmt.Print(string(kubeconfig))
	}

	return nil
}

func newFuncArg(name, value string) goclientnew.FunctionArgument {
	arg := goclientnew.FunctionArgument{
		ParameterName: &name,
		Value:         &goclientnew.FunctionArgument_Value{},
	}
	arg.Value.FromFunctionArgumentValue0(value)
	return arg
}

// encryptedAccessResponse is the JSON structure returned by the generate-kubecontext function.
type encryptedAccessResponse struct {
	PublicKey  string `json:"publicKey"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

const accessHKDFSalt = "confighub-access-v1"

// decryptAccessResponse decrypts a kubeconfig encrypted with ECIES (X25519 + HKDF + AES-256-GCM).
func decryptAccessResponse(privateKey *ecdh.PrivateKey, encryptedJSON []byte) ([]byte, error) {
	var resp encryptedAccessResponse
	if err := json.Unmarshal(encryptedJSON, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse encrypted response")
	}

	workerPubKeyBytes, err := base64.StdEncoding.DecodeString(resp.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode worker public key")
	}

	workerPubKey, err := ecdh.X25519().NewPublicKey(workerPubKeyBytes)
	if err != nil {
		return nil, errors.Wrap(err, "invalid worker public key")
	}

	sharedSecret, err := privateKey.ECDH(workerPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "ECDH failed")
	}

	hkdfReader := hkdf.New(sha256.New, sharedSecret, []byte(accessHKDFSalt), nil)
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return nil, errors.Wrap(err, "HKDF key derivation failed")
	}

	nonce, err := base64.StdEncoding.DecodeString(resp.Nonce)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode nonce")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(resp.Ciphertext)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode ciphertext")
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create AES cipher")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create GCM")
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.Wrap(err, "decryption failed")
	}

	return plaintext, nil
}
