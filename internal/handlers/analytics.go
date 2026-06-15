package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"venue-booking-admin/internal/analytics"
)

func (h *Handler) Aggregate(c *gin.Context) {
	var req analytics.AggRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	if req.StartDate == "" || req.EndDate == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}
	result, err := analytics.Aggregate(h.DB, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Heatmap(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}
	var venueID *uint
	if vid := c.Query("venue_id"); vid != "" {
		v, err := strconv.ParseUint(vid, 10, 32)
		if err == nil {
			u := uint(v)
			venueID = &u
		}
	}
	sportType := c.Query("sport_type")

	result, err := analytics.Heatmap(h.DB, startDate, endDate, venueID, sportType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) WeekdayAnalysis(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}
	var venueID *uint
	if vid := c.Query("venue_id"); vid != "" {
		v, err := strconv.ParseUint(vid, 10, 32)
		if err == nil {
			u := uint(v)
			venueID = &u
		}
	}
	sportType := c.Query("sport_type")

	result, err := analytics.WeekdayAnalysis(h.DB, startDate, endDate, venueID, sportType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Ranking(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}
	sportType := c.Query("sport_type")

	result, err := analytics.Ranking(h.DB, startDate, endDate, sportType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) PeakValley(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}
	sportType := c.Query("sport_type")
	var peakThr, valleyThr float64
	if v := c.Query("peak_threshold"); v != "" {
		peakThr, _ = strconv.ParseFloat(v, 64)
	}
	if v := c.Query("valley_threshold"); v != "" {
		valleyThr, _ = strconv.ParseFloat(v, 64)
	}

	result, err := analytics.PeakValley(h.DB, startDate, endDate, sportType, peakThr, valleyThr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RevenueReport(c *gin.Context) {
	var req analytics.RevenueRequest
	req.StartDate = c.Query("start_date")
	req.EndDate = c.Query("end_date")
	req.Granularity = c.DefaultQuery("granularity", "month")
	req.SportType = c.Query("sport_type")
	req.GroupBy = c.DefaultQuery("group_by", "venue")

	if vid := c.Query("venue_id"); vid != "" {
		v, err := strconv.ParseUint(vid, 10, 32)
		if err == nil {
			u := uint(v)
			req.VenueID = &u
		}
	}

	if req.StartDate == "" || req.EndDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}

	result, err := analytics.RevenueReport(h.DB, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RevenueExport(c *gin.Context) {
	var req analytics.RevenueRequest
	req.StartDate = c.Query("start_date")
	req.EndDate = c.Query("end_date")
	req.Granularity = c.DefaultQuery("granularity", "month")
	req.SportType = c.Query("sport_type")
	req.GroupBy = c.DefaultQuery("group_by", "venue")

	if vid := c.Query("venue_id"); vid != "" {
		v, err := strconv.ParseUint(vid, 10, 32)
		if err == nil {
			u := uint(v)
			req.VenueID = &u
		}
	}

	if req.StartDate == "" || req.EndDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}

	rows, err := analytics.RevenueExport(h.DB, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=revenue_%s_%s.csv", req.StartDate, req.EndDate))

	w := csv.NewWriter(c.Writer)
	for _, row := range rows {
		w.Write(row)
	}
	w.Flush()
}

func (h *Handler) Forecast(c *gin.Context) {
	var venueID *uint
	if vid := c.Query("venue_id"); vid != "" {
		v, err := strconv.ParseUint(vid, 10, 32)
		if err == nil {
			u := uint(v)
			venueID = &u
		}
	}
	days := 14
	if d := c.Query("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			days = n
		}
	}

	result, err := analytics.Forecast(h.DB, venueID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Optimization(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "start_date 和 end_date 必填"})
		return
	}
	sportType := c.Query("sport_type")
	var peakThr, valleyThr float64
	if v := c.Query("peak_threshold"); v != "" {
		peakThr, _ = strconv.ParseFloat(v, 64)
	}
	if v := c.Query("valley_threshold"); v != "" {
		valleyThr, _ = strconv.ParseFloat(v, 64)
	}

	result, err := analytics.Optimize(h.DB, startDate, endDate, sportType, valleyThr, peakThr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func RegisterAnalyticsRoutes(rg *gin.RouterGroup, db *gorm.DB) {
	h := &Handler{DB: db}
	analyticsGroup := rg.Group("/analytics")
	{
		analyticsGroup.POST("/aggregate", h.Aggregate)
		analyticsGroup.GET("/heatmap", h.Heatmap)
		analyticsGroup.GET("/weekday", h.WeekdayAnalysis)
		analyticsGroup.GET("/ranking", h.Ranking)
		analyticsGroup.GET("/peak-valley", h.PeakValley)
		analyticsGroup.GET("/revenue", h.RevenueReport)
		analyticsGroup.GET("/revenue/export", h.RevenueExport)
		analyticsGroup.GET("/forecast", h.Forecast)
		analyticsGroup.GET("/optimization", h.Optimization)
	}
}
