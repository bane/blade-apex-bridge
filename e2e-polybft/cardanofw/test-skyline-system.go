package cardanofw

import (
	"fmt"
)

func NewSkylineSystem(
	dataDirPath string, opts ...ApexSystemOptions,
) (*ApexSystem, error) {
	config := getDefaultSkylinexSystemConfig()
	for _, opt := range opts {
		opt(config)
	}

	users := make([]*TestApexUser, config.UserCnt)

	var err error

	for i := range users {
		users[i], err = NewTestApexUser(
			config.PrimeConfig.NetworkType,
			config.VectorConfig.IsEnabled,
			config.VectorConfig.NetworkType,
			config.NexusConfig.IsEnabled,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create a new skyline user: %w", err)
		}
	}

	apex := &ApexSystem{
		Config:      config,
		Users:       users,
		dataDirPath: dataDirPath,
		chains: []ITestApexChain{
			NewTestCardanoChain(config.PrimeConfig),
			NewTestCardanoChain(config.VectorConfig),
		},
	}

	apex.Config.applyPremineFundingOptions(apex.Users)

	return apex, nil
}
