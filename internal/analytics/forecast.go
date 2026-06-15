package analytics

import (
	"fmt"
	"math"
	"time"

	"gorm.io/gorm"
)

type ForecastResult struct {
	Items []ForecastItem `json:"items"`
}

type ForecastItem struct {
	Date                 string  `json:"date"`
	Weekday              int     `json:"weekday"`
	WeekdayName          string  `json:"weekday_name"`
	PredictedUtilization float64 `json:"predicted_utilization"`
	ConfidenceLower      float64 `json:"confidence_lower"`
	ConfidenceUpper      float64 `json:"confidence_upper"`
	PredictedRevenue     float64 `json:"predicted_revenue"`
}

type dailyStat struct {
	Date            string
	UtilizationRate float64
	Revenue         float64
	Weekday         int
}

func Forecast(db *gorm.DB, venueID *uint, days int) (*ForecastResult, error) {
	if days <= 0 {
		days = 14
	}
	if days > 90 {
		days = 90
	}

	today := time.Now()
	historyStart := today.AddDate(0, 0, -56)
	where := "date BETWEEN ? AND ?"
	args := []interface{}{historyStart.Format("2006-01-02"), today.Format("2006-01-02")}
	if venueID != nil {
		where += " AND venue_id = ?"
		args = append(args, *venueID)
	}

	sqlStr := fmt.Sprintf(
		`SELECT date,
		        ROUND(SUM(booked_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS utilization_rate,
		        ROUND(SUM(revenue), 2) AS revenue
		 FROM stat_hourlies WHERE %s
		 GROUP BY date
		 ORDER BY date`, where,
	)

	raw, err := queryToMaps(db, sqlStr, args...)
	if err != nil {
		return nil, err
	}

	var stats []dailyStat
	for _, r := range raw {
		stats = append(stats, dailyStat{
			Date:            fmt.Sprintf("%v", r["date"]),
			UtilizationRate: toFloat(r["utilization_rate"]),
			Revenue:         toFloat(r["revenue"]),
		})
	}

	for i := range stats {
		t, _ := time.Parse("2006-01-02", stats[i].Date)
		stats[i].Weekday = int(t.Weekday())
		if stats[i].Weekday == 0 {
			stats[i].Weekday = 7
		}
	}

	weekdayFactors := make(map[int]float64)
	weekdayCount := make(map[int]int)
	globalSum := 0.0
	for _, s := range stats {
		globalSum += s.UtilizationRate
		weekdayFactors[s.Weekday] += s.UtilizationRate
		weekdayCount[s.Weekday]++
	}
	globalAvg := 0.0
	if len(stats) > 0 {
		globalAvg = globalSum / float64(len(stats))
	}
	for wd := 1; wd <= 7; wd++ {
		if weekdayCount[wd] > 0 {
			weekdayFactors[wd] = weekdayFactors[wd] / float64(weekdayCount[wd]) / safeDiv(globalAvg)
		} else {
			weekdayFactors[wd] = 1.0
		}
	}

	windowSize := 14
	if len(stats) < windowSize {
		windowSize = len(stats)
	}
	maSum := 0.0
	maRevSum := 0.0
	for i := len(stats) - windowSize; i < len(stats); i++ {
		if i >= 0 {
			maSum += stats[i].UtilizationRate
			maRevSum += stats[i].Revenue
		}
	}
	maUtil := safeDivN(maSum, windowSize)
	maRev := safeDivN(maRevSum, windowSize)

	var residuals []float64
	for i := len(stats) - windowSize; i < len(stats); i++ {
		if i >= 0 {
			predicted := maUtil * weekdayFactors[stats[i].Weekday]
			residuals = append(residuals, stats[i].UtilizationRate-predicted)
		}
	}
	stdDev := 0.0
	if len(residuals) > 1 {
		var sum2 float64
		for _, r := range residuals {
			sum2 += r * r
		}
		stdDev = math.Sqrt(sum2 / float64(len(residuals)))
	}

	var items []ForecastItem
	for d := 1; d <= days; d++ {
		futureDate := today.AddDate(0, 0, d)
		wd := int(futureDate.Weekday())
		if wd == 0 {
			wd = 7
		}

		predUtil := maUtil * weekdayFactors[wd]
		predUtil = math.Max(0, math.Min(100, predUtil))
		predRev := maRev * weekdayFactors[wd]
		predRev = math.Max(0, predRev)

		items = append(items, ForecastItem{
			Date:                 futureDate.Format("2006-01-02"),
			Weekday:              wd,
			WeekdayName:          WeekdayNames[wd],
			PredictedUtilization: round2(predUtil),
			ConfidenceLower:      round2(math.Max(0, predUtil-1.96*stdDev)),
			ConfidenceUpper:      round2(math.Min(100, predUtil+1.96*stdDev)),
			PredictedRevenue:     round2(predRev),
		})
	}

	return &ForecastResult{Items: items}, nil
}

func safeDiv(d float64) float64 {
	if d == 0 {
		return 1
	}
	return d
}

func safeDivN(sum float64, n int) float64 {
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
