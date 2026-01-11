package parser

import (
	"github.com/zc310/ofd/internal/models"
)

type Page struct {
	models.PageContent
	ID models.StID
}
