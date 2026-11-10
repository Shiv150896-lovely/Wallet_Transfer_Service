package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/service"
)

type Handler struct {
	service *service.TransferService
}

func New(transferService *service.TransferService) *Handler {
	return &Handler{service: transferService}
}

func RegisterRoutes(router gin.IRouter, h *Handler) {
	router.POST("/transfers", h.createTransfer)
}

func (h *Handler) createTransfer(c *gin.Context) {
	var req domain.TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	transfer, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidTransfer) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transfer could not be processed"})
		return
	}
	status := http.StatusOK
	if transfer.Status == domain.StatusProcessed {
		status = http.StatusCreated
	}
	c.JSON(status, transfer)
}
