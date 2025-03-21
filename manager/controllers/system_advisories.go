package controllers

import (
	"app/base/database"
	"app/base/models"
	"app/base/utils"
	"app/manager/middlewares"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

var SystemAdvisoriesFields = database.MustGetQueryAttrs(&SystemAdvisoriesDBLookup{})
var SystemAdvisoriesSelect = database.MustGetSelect(&SystemAdvisoriesDBLookup{})
var SystemAdvisoriesOpts = ListOpts{
	Fields:         SystemAdvisoriesFields,
	DefaultFilters: nil,
	DefaultSort:    "-public_date",
	StableSort:     "id",
	SearchFields:   []string{"am.name", "am.synopsis"},
}

type RelList []string

type SystemAdvisoriesDBLookup struct {
	ID string `json:"id" csv:"id" query:"am.name" gorm:"column:id"`
	// a helper to get total number of systems
	MetaTotalHelper

	SystemAdvisoryItemAttributes
}

type SystemAdvisoryItemAttributes struct {
	AdvisoryItemAttributesCommon
	Status *string `json:"status" csv:"status,omitempty" query:"status.name" gorm:"column:status"`
}

type SystemAdvisoryItem struct {
	Attributes SystemAdvisoryItemAttributes `json:"attributes"`
	ID         string                       `json:"id"`
	Type       string                       `json:"type"`
}

type SystemAdvisoriesResponse struct {
	Data  []SystemAdvisoryItem `json:"data"` // advisories items
	Links Links                `json:"links"`
	Meta  ListMeta             `json:"meta"`
}

type AdvisoryStatusID struct {
	AdvisoryID
	SystemAdvisoryStatus
}

func (v RelList) String() string {
	return strings.Join(v, ",")
}

func systemAdvisoriesCommon(c *gin.Context) (*gorm.DB, *ListMeta, []string, error) {
	account := c.GetInt(utils.KeyAccount)
	groups := c.GetStringMapString(utils.KeyInventoryGroups)

	inventoryID := c.Param("inventory_id")
	if inventoryID == "" {
		c.JSON(http.StatusBadRequest, utils.ErrorResponse{Error: "inventory_id param not found"})
		return nil, nil, nil, errors.New("inventory_id param not found")
	}

	if !utils.IsValidUUID(inventoryID) {
		LogAndRespBadRequest(c, errors.New("bad request"), "incorrect inventory_id format")
		return nil, nil, nil, errors.New("incorrect inventory_id format")
	}

	filters, err := ParseAllFilters(c, SystemAdvisoriesOpts)
	if err != nil {
		return nil, nil, nil, err
	} // Error handled method itself

	db := middlewares.DBFromContext(c)
	var exists int64
	err = db.Model(&models.SystemPlatform{}).Where("inventory_id = ?::uuid ", inventoryID).
		Count(&exists).Error

	if err != nil {
		LogAndRespError(c, err, "database error")
		return nil, nil, nil, err
	}
	if exists == 0 {
		err = errors.New("system not found")
		LogAndRespNotFound(c, err, "Systems not found")
		return nil, nil, nil, err
	}

	query := buildSystemAdvisoriesQuery(db, account, groups, inventoryID)
	query, meta, params, err := ListCommon(query, c, filters, SystemAdvisoriesOpts)
	// Error handling and setting of result code & content is done in ListCommon
	return query, meta, params, err
}

// nolint:lll
// @Summary Show me advisories for a system by given inventory id
// @Description Show me advisories for a system by given inventory id
// @ID listSystemAdvisories
// @Security RhIdentity
// @Accept   json
// @Produce  json
// @Param    inventory_id   path    string  true    "Inventory ID"
// @Param    limit          query   int     false   "Limit for paging" minimum(1) maximum(100)
// @Param    offset         query   int     false   "Offset for paging"
// @Param    sort           query   string  false   "Sort field"    Enums(id,name,type,synopsis,public_date)
// @Param    search         query   string  false   "Find matching text"
// @Param    filter[id]                  query   string  false "Filter"
// @Param    filter[description]         query   string  false "Filter"
// @Param    filter[public_date]         query   string  false "Filter"
// @Param    filter[synopsis]            query   string  false "Filter"
// @Param    filter[advisory_type_name]  query   string  false "Filter" Enums(unknown,unspecified,other,enhancement,bugfix,security)
// @Param    filter[severity]            query   int  	 false "Filter" minimum(1) maximum(4)
// @Success 200 {object} SystemAdvisoriesResponse
// @Failure 400 {object} utils.ErrorResponse
// @Failure 404 {object} utils.ErrorResponse
// @Failure 500 {object} utils.ErrorResponse
// @Router /systems/{inventory_id}/advisories [get]
func SystemAdvisoriesHandler(c *gin.Context) {
	query, meta, params, err := systemAdvisoriesCommon(c)
	if err != nil {
		return
	} // Error handled in method itself

	var dbItems []SystemAdvisoriesDBLookup

	if err = query.Find(&dbItems).Error; err != nil {
		LogAndRespError(c, err, "database error")
		return
	}

	data, total := buildSystemAdvisoriesData(dbItems)
	meta, links, err := UpdateMetaLinks(c, meta, total, nil, params...)
	if err != nil {
		return // Error handled in method itself
	}
	var resp = SystemAdvisoriesResponse{
		Data:  data,
		Links: *links,
		Meta:  *meta,
	}
	c.JSON(http.StatusOK, &resp)
}

// nolint:lll
// @Summary Show me advisories for a system by given inventory id
// @Description Show me advisories for a system by given inventory id
// @ID listSystemAdvisoriesIds
// @Security RhIdentity
// @Accept   json
// @Produce  json
// @Param    inventory_id   path    string  true    "Inventory ID"
// @Param    limit          query   int     false   "Limit for paging" minimum(1) maximum(100)
// @Param    offset         query   int     false   "Offset for paging"
// @Param    sort           query   string  false   "Sort field"    Enums(id,name,type,synopsis,public_date)
// @Param    search         query   string  false   "Find matching text"
// @Param    filter[id]                  query   string  false "Filter"
// @Param    filter[description]         query   string  false "Filter"
// @Param    filter[public_date]         query   string  false "Filter"
// @Param    filter[synopsis]            query   string  false "Filter"
// @Param    filter[advisory_type_name]  query   string  false "Filter" Enums(unknown,unspecified,other,enhancement,bugfix,security)
// @Param    filter[severity]            query   int  	 false "Filter" minimum(1) maximum(4)
// @Success 200 {object} IDsStatusResponse
// @Failure 400 {object} utils.ErrorResponse
// @Failure 404 {object} utils.ErrorResponse
// @Failure 500 {object} utils.ErrorResponse
// @Router /ids/systems/{inventory_id}/advisories [get]
func SystemAdvisoriesIDsHandler(c *gin.Context) {
	query, _, _, err := systemAdvisoriesCommon(c)
	if err != nil {
		return
	} // Error handled in method itself

	var aids []AdvisoryStatusID
	err = query.Find(&aids).Error
	if err != nil {
		LogAndRespError(c, err, "db error")
	}

	resp := advisoriesStatusIDs(aids)
	c.JSON(http.StatusOK, &resp)
}

func buildSystemAdvisoriesQuery(db *gorm.DB, account int, groups map[string]string, inventoryID string) *gorm.DB {
	query := database.SystemAdvisoriesByInventoryID(db, account, groups, inventoryID,
		database.JoinAdvisoryMetadata, database.JoinAdvisoryType).
		Joins("JOIN status ON sa.status_id = status.id").
		Select(SystemAdvisoriesSelect)
	return query
}

func buildSystemAdvisoriesData(models []SystemAdvisoriesDBLookup) ([]SystemAdvisoryItem, int) {
	var total int
	if len(models) > 0 {
		total = models[0].Total
	}
	data := make([]SystemAdvisoryItem, len(models))
	for i, advisory := range models {
		advisory.AdvisoryItemAttributesCommon = fillAdvisoryItemAttributeReleaseVersion(advisory.AdvisoryItemAttributesCommon)
		item := SystemAdvisoryItem{
			ID:         advisory.ID,
			Type:       "advisory",
			Attributes: advisory.SystemAdvisoryItemAttributes,
		}
		data[i] = item
	}
	return data, total
}
