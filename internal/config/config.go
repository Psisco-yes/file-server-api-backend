package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	DB      DBConfig      `mapstructure:"db"`
	JWT     JWTConfig     `mapstructure:"jwt"`
	Storage StorageConfig `mapstructure:"storage"`
	AppHost string        `mapstructure:"host"`
}

type DBConfig struct {
	Source string `mapstructure:"source"`
}

type JWTConfig struct {
	Secret string `mapstructure:"secret"`
}

type StorageConfig struct {
	Path string `mapstructure:"path"`
}

func Load() (*Config, error) {
	viper.AddConfigPath("./configs")
	viper.AddConfigPath("/configs")
	viper.SetConfigName("settings")
	viper.SetConfigType("yml")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
