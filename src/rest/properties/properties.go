package properties

import "time"

// AppProperties stores runtime settings for REST layer.
type AppProperties struct {
	ServerPort          string
	JWTSecret           string
	JWTIssuer           string
	JWTTTL              time.Duration
	JWTRefreshTTL       time.Duration
	AuthMessageTemplate string
	InitiaRPCURL        string
	DBURL               string
}
