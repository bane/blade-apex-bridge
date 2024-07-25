package alibabassm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/0xPolygon/polygon-edge/secrets"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	oos20190601 "github.com/alibabacloud-go/oos-20190601/v4/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/hashicorp/go-hclog"
)

type AlibabaSsmManager struct {
	// Local logger object
	logger hclog.Logger

	// The Alibaba region
	region string

	// Custom Alibaba endpoint, e.g. localstack
	endpoint string

	// The Alibaba SDK client
	client *oos20190601.Client

	// The base path to store the secrets in SSM Parameter Store
	basePath string
}

func SecretsManagerFactory(
	config *secrets.SecretsManagerConfig,
	params *secrets.SecretsManagerParams) (secrets.SecretsManager, error) { //nolint

	// Check if the node name is present
	if config.Name == "" {
		return nil, errors.New("no node name specified for Alibaba SSM secrets manager")
	}

	// Check if the extra map is present
	if config.Extra == nil || config.Extra["region"] == nil || config.Extra["ssm-parameter-path"] == nil {
		return nil, errors.New("required extra map containing 'region' and 'ssm-parameter-path' not found for aws-ssm")
	}

	// / Set up the base object
	alibabaSsmManager := &AlibabaSsmManager{
		logger:   params.Logger.Named(string(secrets.AlibabaSSM)),
		region:   fmt.Sprintf("%v", config.Extra["region"]),
		endpoint: config.ServerURL,
	}

	// Set the base path to store the secrets in SSM
	alibabaSsmManager.basePath = fmt.Sprintf("%s/%s", config.Extra["ssm-parameter-path"], config.Name)

	// Run the initial setup
	if err := alibabaSsmManager.Setup(); err != nil {
		return nil, err
	}

	return alibabaSsmManager, nil
}

// Setup sets up the Alibaba SSM secrets manager
func (a *AlibabaSsmManager) Setup() error {
	config := &openapi.Config{
		// Required, please ensure that the environment variables ALIBABA_CLOUD_ACCESS_KEY_ID is set.
		AccessKeyId: tea.String(os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")),
		// Required, please ensure that the environment variables ALIBABA_CLOUD_ACCESS_KEY_SECRET is set.
		AccessKeySecret: tea.String(os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")),
		// config.Endpoint = tea.String("oos.eu-central-1.aliyuncs.com")
		Endpoint: tea.String(a.endpoint),
		// eu-central-1
		RegionId: tea.String(a.region),
	}

	client, err := oos20190601.NewClient(config)
	if err != nil {
		return err
	}

	a.client = client

	return nil
}

// constructSecretPath is a helper method for constructing a path to the secret
func (a *AlibabaSsmManager) constructSecretPath(name string) string {
	return fmt.Sprintf("%s/%s", a.basePath, name)
}

// GetSecret fetches a secret from Alibaba SSM
func (a *AlibabaSsmManager) GetSecret(name string) ([]byte, error) {
	getSecretParameterRequest := &oos20190601.GetSecretParameterRequest{
		RegionId:       tea.String(a.region), // eu-central-1
		Name:           tea.String(a.constructSecretPath(name)),
		WithDecryption: tea.Bool(true),
	}
	runtime := &util.RuntimeOptions{}
	retVal, tryErr := func() (_b []byte, _e error) {
		defer func() {
			if r := tea.Recover(recover()); r != nil {
				_b = nil
				_e = r
			}
		}()

		response, err := a.client.GetSecretParameterWithOptions(getSecretParameterRequest, runtime)
		if err != nil {
			return nil, err
		}

		return []byte(tea.StringValue(response.Body.Parameter.Value)), nil
	}()

	if tryErr != nil {
		a.logError(tryErr)
	}

	return retVal, tryErr
}

// SetSecret saves a secret to Alibaba SSM
func (a *AlibabaSsmManager) SetSecret(name string, value []byte) error {
	createSecretParameterRequest := &oos20190601.CreateSecretParameterRequest{
		RegionId: tea.String(a.region), // eu-central-1
		Name:     tea.String(a.constructSecretPath(name)),
		Value:    tea.String(string(value)),
	}
	runtime := &util.RuntimeOptions{}
	tryErr := func() (_e error) {
		defer func() {
			if r := tea.Recover(recover()); r != nil {
				_e = r
			}
		}()

		_, err := a.client.CreateSecretParameterWithOptions(createSecretParameterRequest, runtime)
		if err != nil {
			return err
		}

		return nil
	}()

	if tryErr != nil {
		a.logError(tryErr)
	}

	return tryErr
}

// HasSecret checks if the secret is present on Alibabab SSM ParameterStore
func (a *AlibabaSsmManager) HasSecret(name string) bool {
	_, err := a.GetSecret(name)

	return err == nil
}

// RemoveSecret removes a secret from Alibaba SSM ParameterStore
func (a *AlibabaSsmManager) RemoveSecret(name string) error {
	deleteSecretParameterRequest := &oos20190601.DeleteSecretParameterRequest{
		RegionId: tea.String(a.region),
		Name:     tea.String(a.constructSecretPath(name)),
	}
	runtime := &util.RuntimeOptions{}
	tryErr := func() (_e error) {
		defer func() {
			if r := tea.Recover(recover()); r != nil {
				_e = r
			}
		}()

		_, err := a.client.DeleteSecretParameterWithOptions(deleteSecretParameterRequest, runtime)
		if err != nil {
			return err
		}

		return nil
	}()

	if tryErr != nil {
		a.logError(tryErr)
	}

	return tryErr
}

func (a *AlibabaSsmManager) logError(err error) {
	var e *tea.SDKError
	if ok := errors.As(err, &e); !ok {
		e = &tea.SDKError{Message: tea.String(err.Error())}
	}

	_, err = util.AssertAsString(e.Message)
	if err != nil {
		a.logger.Error("unable to log error message")

		return
	}

	a.logger.Error(tea.StringValue(e.Message))

	var data interface{}

	d := json.NewDecoder(strings.NewReader(tea.StringValue(e.Data)))

	err = d.Decode(&data)
	if err != nil {
		a.logger.Error("unable to decode recommendation", err)

		return
	}

	if m, ok := data.(map[string]interface{}); ok {
		recommend := m["Recommend"]
		a.logger.Info("recommend", recommend)
	}
}
