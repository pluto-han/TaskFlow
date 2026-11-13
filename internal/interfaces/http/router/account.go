package interfaceshttprouter

import (
	"github.com/gin-gonic/gin"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	interfaceshttpaccount "github.com/pluto-han/taskflow/internal/interfaces/http/account"
)

func RegisterAccounts(router *gin.Engine, service *applicationaccount.Service) {
	router.POST("/auth/register", interfaceshttpaccount.New(service).Register)
}
