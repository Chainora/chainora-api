package properties

import "time"

// AppProperties stores runtime settings for REST layer.
type AppProperties struct {
	ServerPort                     string
	AllowedOrigins                 []string
	MaxRequestBodyBytes            int64
	AllowEmptyOriginForWS          bool
	JWTSecret                      string
	JWTIssuer                      string
	JWTTTL                         time.Duration
	JWTRefreshTTL                  time.Duration
	AuthMessageTemplate            string
	InitiaRPCURL                   string
	InitiaAPIURL                   string
	DBURL                          string
	RelayerMasterPrivateKey        string
	RelayerMasterAddress           string
	RelayerMessageTemplate         string
	RelayerPrimaryMessageTemplate  string
	RelayerInitiadBinary           string
	RelayerChainID                 string
	RelayerNodeURL                 string
	RelayerKeyName                 string
	RelayerKeyringBackend          string
	RelayerHome                    string
	RelayerGasPrices               string
	RelayerMoveModuleAddr          string
	RelayerMoveModuleName          string
	RelayerMoveFunctionName        string
	RelayerMoveArgsJSON            string
	RelayerMoveTypeArgsJSON        string
	RelayerMovePrimaryFunctionName string
	RelayerMovePrimaryArgsJSON     string
	RelayerMovePrimaryTypeArgsJSON string
	RelayerDryRun                  bool
	CardFactoryRootPublicKey       string
}
