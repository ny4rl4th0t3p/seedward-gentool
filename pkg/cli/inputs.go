package cli

import (
	"fmt"

	"github.com/spf13/viper"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
)

// Inputs are the genesis-construction inputs named by a gentool config file: the chain config
// plus the on-disk locations of each allocation CSV and the gentx directory. An empty path
// field means the config did not set that (optional) input.
type Inputs struct {
	Chain    genesis.ChainConfig
	GentxDir string
	Accounts string
	Claims   string
	Grants   string
	Authz    string
	Feegrant string
}

// LoadInputs parses a gentool config file (the same file `gentool create` consumes) into its
// genesis-construction Inputs, so external tools — e.g. the rehearsal runner — can assemble the
// same input set without duplicating the YAML schema. Unlike the create command it requires an
// explicit path and errors if the file cannot be read.
func LoadInputs(cfgFile string) (Inputs, error) {
	if cfgFile == "" {
		return Inputs{}, fmt.Errorf("config file path is required")
	}
	v := viper.New()
	v.SetConfigFile(cfgFile)
	v.AutomaticEnv()
	if err := v.ReadInConfig(); err != nil {
		return Inputs{}, fmt.Errorf("read config %s: %w", cfgFile, err)
	}
	return inputsFromViper(v)
}

// inputsFromViper maps an already-loaded viper into Inputs. It is the single viper→Inputs
// path, shared by LoadInputs (external callers) and the create command, so the two never drift.
func inputsFromViper(v *viper.Viper) (Inputs, error) {
	hrp := v.GetString("chain.address_prefix")
	if hrp == "" {
		return Inputs{}, fmt.Errorf("chain.address_prefix is required in config")
	}
	return Inputs{
		Chain:    buildChainConfig(v, hrp),
		GentxDir: v.GetString("validators.gentx_dir"),
		Accounts: v.GetString("accounts.file_name"),
		Claims:   v.GetString("claims.file_name"),
		Grants:   v.GetString("grants.file_name"),
		Authz:    v.GetString("authz.file_name"),
		Feegrant: v.GetString("feegrant.file_name"),
	}, nil
}
