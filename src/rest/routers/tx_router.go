package routers

import (
	"chainora-api/rest/controllers"

	"github.com/gin-gonic/gin"
)

func RegisterTxRoutes(v1 *gin.RouterGroup, controller *controllers.TxController) {
	tx := v1.Group("/tx")
	tx.POST("/decode", controller.DecodeTx)
	tx.POST("/typed-data", controller.BuildTransferTypedData)
}
