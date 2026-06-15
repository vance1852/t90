package analytics

import (
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

type HeatmapResult struct {
	Hours  []int           `json:"hours"`
	Venues []HeatmapVenue  `json:"venues"`
}

type HeatmapVenue struct {
	VenueID   uint              `json:"venue_id"`
	VenueName string            `json:"venue_name"`
	SportType string            `json:"sport_type"`
	Data      map[string]float64 `json:"data"`
}

func Heatmap(db *gorm.DB, startDate, endDate string, venueID *uint, sportType string) (*HeatmapResult, error) {
	where := "date BETWEEN ? AND ?"
	args := []interface{}{startDate, endDate}
	if venueID != nil {
		where += " AND venue_id = ?"
		args = append(args, *venueID)
	}
	if sportType != "" {
		where += " AND sport_type = ?"
		args = append(args, sportType)
	}

	sqlStr := fmt.Sprintf(
		`SELECT venue_id, venue_name, sport_type, hour,
		        ROUND(SUM(booked_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS utilization_rate
		 FROM stat_hourlies WHERE %s
		 GROUP BY venue_id, venue_name, sport_type, hour
		 ORDER BY venue_id, hour`, where,
	)

	raw, err := queryToMaps(db, sqlStr, args...)
	if err != nil {
		return nil, err
	}

	venueMap := make(map[uint]*HeatmapVenue)
	var venueOrder []uint
	hourSet := make(map[int]bool)

	for _, r := range raw {
		vid := uint(toInt(r["venue_id"]))
		hour := toInt(r["hour"])
		hourSet[hour] = true

		v, ok := venueMap[vid]
		if !ok {
			v = &HeatmapVenue{
				VenueID:   vid,
				VenueName: fmt.Sprintf("%v", r["venue_name"]),
				SportType: fmt.Sprintf("%v", r["sport_type"]),
				Data:      make(map[string]float64),
			}
			venueMap[vid] = v
			venueOrder = append(venueOrder, vid)
		}
		v.Data[fmt.Sprintf("%d", hour)] = toFloat(r["utilization_rate"])
	}

	var hours []int
	for h := range hourSet {
		hours = append(hours, h)
	}
	sort.Ints(hours)

	var venues []HeatmapVenue
	for _, vid := range venueOrder {
		venues = append(venues, *venueMap[vid])
	}

	return &HeatmapResult{Hours: hours, Venues: venues}, nil
}

type WeekdayResult struct {
	Items []WeekdayItem `json:"items"`
}

type WeekdayItem struct {
	Weekday        int     `json:"weekday"`
	WeekdayName    string  `json:"weekday_name"`
	TotalSlots     int     `json:"total_slots"`
	BookedSlots    int     `json:"booked_slots"`
	UtilizationRate float64 `json:"utilization_rate"`
	Revenue        float64  `json:"revenue"`
	AvgPrice       float64  `json:"avg_price"`
	CancelRate     float64  `json:"cancel_rate"`
}

func WeekdayAnalysis(db *gorm.DB, startDate, endDate string, venueID *uint, sportType string) (*WeekdayResult, error) {
	where := "date BETWEEN ? AND ?"
	args := []interface{}{startDate, endDate}
	if venueID != nil {
		where += " AND venue_id = ?"
		args = append(args, *venueID)
	}
	if sportType != "" {
		where += " AND sport_type = ?"
		args = append(args, sportType)
	}

	sqlStr := fmt.Sprintf(
		`SELECT weekday,
		        SUM(total_slots) AS total_slots,
		        SUM(booked_slots) AS booked_slots,
		        ROUND(SUM(booked_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS utilization_rate,
		        ROUND(SUM(revenue), 2) AS revenue,
		        ROUND(SUM(revenue) / NULLIF(SUM(booked_slots), 0), 2) AS avg_price,
		        ROUND(SUM(cancelled_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS cancel_rate
		 FROM stat_hourlies WHERE %s
		 GROUP BY weekday
		 ORDER BY weekday`, where,
	)

	raw, err := queryToMaps(db, sqlStr, args...)
	if err != nil {
		return nil, err
	}

	var items []WeekdayItem
	for _, r := range raw {
		wd := toInt(r["weekday"])
		items = append(items, WeekdayItem{
			Weekday:        wd,
			WeekdayName:    WeekdayNames[wd],
			TotalSlots:     toInt(r["total_slots"]),
			BookedSlots:    toInt(r["booked_slots"]),
			UtilizationRate: toFloat(r["utilization_rate"]),
			Revenue:        toFloat(r["revenue"]),
			AvgPrice:       toFloat(r["avg_price"]),
			CancelRate:     toFloat(r["cancel_rate"]),
		})
	}
	return &WeekdayResult{Items: items}, nil
}

type RankingResult struct {
	ByUtilization []RankingItem `json:"by_utilization"`
	ByRevenue     []RankingItem `json:"by_revenue"`
}

type RankingItem struct {
	VenueID        uint    `json:"venue_id"`
	VenueName      string  `json:"venue_name"`
	SportType      string  `json:"sport_type"`
	TotalSlots     int     `json:"total_slots"`
	BookedSlots    int     `json:"booked_slots"`
	UtilizationRate float64 `json:"utilization_rate"`
	Revenue        float64  `json:"revenue"`
	AvgPrice       float64  `json:"avg_price"`
	CancelRate     float64  `json:"cancel_rate"`
}

func Ranking(db *gorm.DB, startDate, endDate string, sportType string) (*RankingResult, error) {
	where := "date BETWEEN ? AND ?"
	args := []interface{}{startDate, endDate}
	if sportType != "" {
		where += " AND sport_type = ?"
		args = append(args, sportType)
	}

	sqlStr := fmt.Sprintf(
		`SELECT venue_id, venue_name, sport_type,
		        SUM(total_slots) AS total_slots,
		        SUM(booked_slots) AS booked_slots,
		        ROUND(SUM(booked_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS utilization_rate,
		        ROUND(SUM(revenue), 2) AS revenue,
		        ROUND(SUM(revenue) / NULLIF(SUM(booked_slots), 0), 2) AS avg_price,
		        ROUND(SUM(cancelled_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS cancel_rate
		 FROM stat_hourlies WHERE %s
		 GROUP BY venue_id, venue_name, sport_type`, where,
	)

	raw, err := queryToMaps(db, sqlStr, args...)
	if err != nil {
		return nil, err
	}

	var items []RankingItem
	for _, r := range raw {
		items = append(items, RankingItem{
			VenueID:        uint(toInt(r["venue_id"])),
			VenueName:      fmt.Sprintf("%v", r["venue_name"]),
			SportType:      fmt.Sprintf("%v", r["sport_type"]),
			TotalSlots:     toInt(r["total_slots"]),
			BookedSlots:    toInt(r["booked_slots"]),
			UtilizationRate: toFloat(r["utilization_rate"]),
			Revenue:        toFloat(r["revenue"]),
			AvgPrice:       toFloat(r["avg_price"]),
			CancelRate:     toFloat(r["cancel_rate"]),
		})
	}

	byUtil := make([]RankingItem, len(items))
	copy(byUtil, items)
	sort.Slice(byUtil, func(i, j int) bool { return byUtil[i].UtilizationRate > byUtil[j].UtilizationRate })

	byRev := make([]RankingItem, len(items))
	copy(byRev, items)
	sort.Slice(byRev, func(i, j int) bool { return byRev[i].Revenue > byRev[j].Revenue })

	return &RankingResult{ByUtilization: byUtil, ByRevenue: byRev}, nil
}

type PeakValleyResult struct {
	Peaks   []PeakValleyItem `json:"peaks"`
	Valleys []PeakValleyItem `json:"valleys"`
}

type PeakValleyItem struct {
	VenueID        uint    `json:"venue_id"`
	VenueName      string  `json:"venue_name"`
	SportType      string  `json:"sport_type"`
	Weekday        int     `json:"weekday"`
	WeekdayName    string  `json:"weekday_name"`
	Hour           int     `json:"hour"`
	UtilizationRate float64 `json:"utilization_rate"`
	AvgRevenue     float64 `json:"avg_revenue"`
	Occurrences    int     `json:"occurrences"`
}

func PeakValley(db *gorm.DB, startDate, endDate string, sportType string, peakThr, valleyThr float64) (*PeakValleyResult, error) {
	if peakThr == 0 {
		peakThr = 85
	}
	if valleyThr == 0 {
		valleyThr = 30
	}

	where := "date BETWEEN ? AND ?"
	args := []interface{}{startDate, endDate}
	if sportType != "" {
		where += " AND sport_type = ?"
		args = append(args, sportType)
	}

	sqlStr := fmt.Sprintf(
		`SELECT venue_id, venue_name, sport_type, weekday, hour,
		        SUM(total_slots) AS total_slots,
		        SUM(booked_slots) AS booked_slots,
		        ROUND(SUM(booked_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS utilization_rate,
		        ROUND(AVG(revenue), 2) AS avg_revenue,
		        COUNT(*) AS occurrences
		 FROM stat_hourlies WHERE %s
		 GROUP BY venue_id, venue_name, sport_type, weekday, hour
		 HAVING SUM(total_slots) >= 4
		 ORDER BY venue_id, weekday, hour`, where,
	)

	raw, err := queryToMaps(db, sqlStr, args...)
	if err != nil {
		return nil, err
	}

	var peaks, valleys []PeakValleyItem
	for _, r := range raw {
		wd := toInt(r["weekday"])
		item := PeakValleyItem{
			VenueID:        uint(toInt(r["venue_id"])),
			VenueName:      fmt.Sprintf("%v", r["venue_name"]),
			SportType:      fmt.Sprintf("%v", r["sport_type"]),
			Weekday:        wd,
			WeekdayName:    WeekdayNames[wd],
			Hour:           toInt(r["hour"]),
			UtilizationRate: toFloat(r["utilization_rate"]),
			AvgRevenue:     toFloat(r["avg_revenue"]),
			Occurrences:    toInt(r["occurrences"]),
		}
		if item.UtilizationRate >= peakThr {
			peaks = append(peaks, item)
		} else if item.UtilizationRate <= valleyThr {
			valleys = append(valleys, item)
		}
	}

	sort.Slice(peaks, func(i, j int) bool { return peaks[i].UtilizationRate > peaks[j].UtilizationRate })
	sort.Slice(valleys, func(i, j int) bool { return valleys[i].UtilizationRate < valleys[j].UtilizationRate })

	return &PeakValleyResult{Peaks: peaks, Valleys: valleys}, nil
}

func JoinInts(ints []int, sep string) string {
	var ss []string
	for _, i := range ints {
		ss = append(ss, fmt.Sprintf("%d", i))
	}
	return strings.Join(ss, sep)
}
