package querycheck

import (
	"time"

	"github.com/labstack/echo/v4"
)

type RetQuery struct {
	Name        string     `json:"name"`
	Query       string     `json:"query"`
	LastPlan    *QueryPlan `json:"last_plan"`
	LastChecked time.Time  `json:"last_checked"`
	LastError   error      `json:"last_error"`
	CheckEvery  string     `json:"check_every"`
}

func (q *Querychecker) HandleGetQueries(c echo.Context) error {
	queries := q.GetQueries(c.Request().Context())
	retQueries := []RetQuery{}
	for _, query := range queries {
		retQueries = append(retQueries, RetQuery{
			Name:        query.Name,
			Query:       query.Query,
			LastPlan:    query.LastPlan,
			LastChecked: query.LastChecked,
			LastError:   query.LastError,
			CheckEvery:  query.CheckEvery.String(),
		})
	}

	return c.JSON(200, retQueries)
}

func (q *Querychecker) HandleGetQuery(c echo.Context) error {
	query := q.GetQuery(c.Request().Context(), c.Param("name"))
	if query == nil {
		return c.JSON(404, echo.Map{
			"message": "not found",
		})
	}

	retQuery := RetQuery{
		Name:        query.Name,
		Query:       query.Query,
		LastPlan:    query.LastPlan,
		LastChecked: query.LastChecked,
		LastError:   query.LastError,
		CheckEvery:  query.CheckEvery.String(),
	}

	return c.JSON(200, retQuery)
}

func (q *Querychecker) HandleDeleteQuery(c echo.Context) error {
	q.RemoveQuery(c.Request().Context(), c.Param("name"))
	return c.JSON(200, echo.Map{
		"message": "success",
	})
}

type AddQueryRequest struct {
	Name       string `json:"name"`
	Query      string `json:"query"`
	CheckEvery int64  `json:"check_every_ms"`
}

func (q *Querychecker) HandleAddQuery(c echo.Context) error {
	var req AddQueryRequest
	c.Bind(&req)
	q.AddQuery(c.Request().Context(), req.Name, req.Query, time.Duration(req.CheckEvery)*time.Millisecond)
	return c.JSON(200, echo.Map{
		"name":           req.Name,
		"query":          req.Query,
		"check_every_ms": req.CheckEvery,
		"message":        "success",
	})
}

type UpdateQueryRequest struct {
	Query      string `json:"query"`
	CheckEvery int64  `json:"check_every_ms"`
}

func (q *Querychecker) HandleUpdateQuery(c echo.Context) error {
	var req UpdateQueryRequest
	c.Bind(&req)
	q.UpdateQuery(c.Request().Context(), c.Param("name"), req.Query, time.Duration(req.CheckEvery)*time.Millisecond)
	return c.JSON(200, echo.Map{
		"name":           c.Param("name"),
		"query":          req.Query,
		"check_every_ms": req.CheckEvery,
		"message":        "success",
	})
}
