package analytics

import (
	"fmt"
	"math"
	"sort"

	"gorm.io/gorm"
)

type OptimizationResult struct {
	Suggestions []Suggestion `json:"suggestions"`
}

type Suggestion struct {
	VenueID               uint    `json:"venue_id"`
	VenueName             string  `json:"venue_name"`
	SportType             string  `json:"sport_type"`
	Weekday               int     `json:"weekday"`
	WeekdayName           string  `json:"weekday_name"`
	Hours                 []int   `json:"hours"`
	CurrentUtilization    float64 `json:"current_utilization"`
	CurrentRevenue        float64 `json:"current_revenue"`
	SuggestionType        string  `json:"suggestion_type"`
	SuggestionDetail      string  `json:"suggestion_detail"`
	PriceAdjustment       float64 `json:"price_adjustment"`
	ExpectedUtilization   float64 `json:"expected_utilization"`
	ExpectedRevenue       float64 `json:"expected_revenue"`
	ExpectedRevenueChange float64 `json:"expected_revenue_change"`
	MonthlyRevenueChange  float64 `json:"monthly_revenue_change"`
	Confidence            string  `json:"confidence"`
}

type slotProfile struct {
	VenueID        uint
	VenueName      string
	SportType      string
	Weekday        int
	Hour           int
	UtilizationRate float64
	AvgRevenue     float64
	Occurrences    int
	HourlyPrice    float64
}

func Optimize(db *gorm.DB, startDate, endDate string, sportType string, valleyThr, peakThr float64) (*OptimizationResult, error) {
	if valleyThr == 0 {
		valleyThr = 30
	}
	if peakThr == 0 {
		peakThr = 85
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

	var profiles []slotProfile
	for _, r := range raw {
		profiles = append(profiles, slotProfile{
			VenueID:        uint(toInt(r["venue_id"])),
			VenueName:      fmt.Sprintf("%v", r["venue_name"]),
			SportType:      fmt.Sprintf("%v", r["sport_type"]),
			Weekday:        toInt(r["weekday"]),
			Hour:           toInt(r["hour"]),
			UtilizationRate: toFloat(r["utilization_rate"]),
			AvgRevenue:     toFloat(r["avg_revenue"]),
			Occurrences:    toInt(r["occurrences"]),
		})
	}

	venues := getVenueHourlyPrices(db)
	for i := range profiles {
		if p, ok := venues[profiles[i].VenueID]; ok {
			profiles[i].HourlyPrice = p
		}
	}

	grouped := groupConsecutiveSlots(profiles, valleyThr, peakThr)

	var suggestions []Suggestion
	for _, g := range grouped {
		sug := buildSuggestion(g, valleyThr, peakThr)
		if sug != nil {
			suggestions = append(suggestions, *sug)
		}
	}

	sort.Slice(suggestions, func(i, j int) bool {
		absI := math.Abs(suggestions[i].MonthlyRevenueChange)
		absJ := math.Abs(suggestions[j].MonthlyRevenueChange)
		return absI > absJ
	})

	return &OptimizationResult{Suggestions: suggestions}, nil
}

type slotGroup struct {
	VenueID        uint
	VenueName      string
	SportType      string
	Weekday        int
	Hours          []int
	UtilizationAvg float64
	AvgRevenueSum  float64
	HourlyPrice    float64
	Type           string
	Occurrences    int
}

func groupConsecutiveSlots(profiles []slotProfile, valleyThr, peakThr float64) []slotGroup {
	type groupKey struct {
		VenueID uint
		Weekday int
		Type    string
	}

	keyMap := make(map[groupKey][]slotProfile)
	for _, p := range profiles {
		var typ string
		if p.UtilizationRate <= valleyThr {
			typ = "valley"
		} else if p.UtilizationRate >= peakThr {
			typ = "peak"
		} else {
			continue
		}
		k := groupKey{VenueID: p.VenueID, Weekday: p.Weekday, Type: typ}
		keyMap[k] = append(keyMap[k], p)
	}

	var groups []slotGroup
	for k, slots := range keyMap {
		sort.Slice(slots, func(i, j int) bool { return slots[i].Hour < slots[j].Hour })

		var current []slotProfile
		for _, s := range slots {
			if len(current) == 0 || s.Hour == current[len(current)-1].Hour+1 {
				current = append(current, s)
			} else {
				if len(current) >= 2 {
					groups = append(groups, makeGroup(current, k.Type))
				}
				current = []slotProfile{s}
			}
		}
		if len(current) >= 2 {
			groups = append(groups, makeGroup(current, k.Type))
		}
		if len(slots) >= 1 && len(current) < 2 {
			groups = append(groups, makeGroup(slots[:1], k.Type))
		}
	}

	return groups
}

func makeGroup(slots []slotProfile, typ string) slotGroup {
	var hours []int
	var utilSum, revSum float64
	price := 0.0
	occ := 0
	for _, s := range slots {
		hours = append(hours, s.Hour)
		utilSum += s.UtilizationRate
		revSum += s.AvgRevenue
		if s.HourlyPrice > 0 {
			price = s.HourlyPrice
		}
		occ += s.Occurrences
	}
	return slotGroup{
		VenueID:        slots[0].VenueID,
		VenueName:      slots[0].VenueName,
		SportType:      slots[0].SportType,
		Weekday:        slots[0].Weekday,
		Hours:          hours,
		UtilizationAvg: utilSum / float64(len(slots)),
		AvgRevenueSum:  revSum,
		HourlyPrice:    price,
		Type:           typ,
		Occurrences:    occ,
	}
}

func buildSuggestion(g slotGroup, valleyThr, peakThr float64) *Suggestion {
	if g.HourlyPrice == 0 {
		g.HourlyPrice = 100
	}

	hoursPerWeek := float64(len(g.Hours))
	currentWeeklyRev := g.AvgRevenueSum
	currentUtil := g.UtilizationAvg

	var sug Suggestion
	sug.VenueID = g.VenueID
	sug.VenueName = g.VenueName
	sug.SportType = g.SportType
	sug.Weekday = g.Weekday
	sug.WeekdayName = WeekdayNames[g.Weekday]
	sug.Hours = g.Hours
	sug.CurrentUtilization = round2(currentUtil)
	sug.CurrentRevenue = round2(currentWeeklyRev)

	if g.Type == "valley" {
		elasticity := 1.5
		discountPct := 0.3
		priceAdj := round2(g.HourlyPrice * (1 - discountPct))

		demandIncrease := discountPct * elasticity
		expectedUtil := currentUtil * (1 + demandIncrease)
		expectedUtil = math.Min(expectedUtil, 60)
		if expectedUtil < currentUtil*1.1 {
			expectedUtil = currentUtil * 1.1
		}

		expectedWeeklyRev := hoursPerWeek * expectedUtil / 100.0 * priceAdj
		revChange := expectedWeeklyRev - currentWeeklyRev
		monthlyChange := revChange * 4.3

		sug.SuggestionType = "discount"
		sug.PriceAdjustment = priceAdj
		sug.ExpectedUtilization = round2(expectedUtil)
		sug.ExpectedRevenue = round2(expectedWeeklyRev)
		sug.ExpectedRevenueChange = round2(revChange)
		sug.MonthlyRevenueChange = round2(monthlyChange)

		hourStr := JoinInts(g.Hours, ":00-") + ":00"
		sug.SuggestionDetail = fmt.Sprintf(
			"%s %s %s 长期空置（利用率%.1f%%），建议推出低谷折扣（%.0f折，单价%.0f元/时），预计利用率可提升至%.1f%%，月增收约%.0f元",
			g.VenueName, sug.WeekdayName, hourStr,
			currentUtil, (1-discountPct)*10, priceAdj,
			expectedUtil, monthlyChange,
		)

		if g.Occurrences >= 12 {
			sug.Confidence = "high"
		} else if g.Occurrences >= 6 {
			sug.Confidence = "medium"
		} else {
			sug.Confidence = "low"
		}
	} else {
		premiumPct := 0.2
		priceAdj := round2(g.HourlyPrice * (1 + premiumPct))

		demandDrop := premiumPct * 0.5
		expectedUtil := currentUtil * (1 - demandDrop)
		expectedUtil = math.Max(expectedUtil, 70)

		expectedWeeklyRev := hoursPerWeek * expectedUtil / 100.0 * priceAdj
		revChange := expectedWeeklyRev - currentWeeklyRev
		monthlyChange := revChange * 4.3

		sug.SuggestionType = "premium"
		sug.PriceAdjustment = priceAdj
		sug.ExpectedUtilization = round2(expectedUtil)
		sug.ExpectedRevenue = round2(expectedWeeklyRev)
		sug.ExpectedRevenueChange = round2(revChange)
		sug.MonthlyRevenueChange = round2(monthlyChange)

		hourStr := JoinInts(g.Hours, ":00-") + ":00"
		sug.SuggestionDetail = fmt.Sprintf(
			"%s %s %s 长期满场（利用率%.1f%%），建议高峰加价（%.0f%%，单价%.0f元/时），预计利用率微降至%.1f%%，月增收约%.0f元",
			g.VenueName, sug.WeekdayName, hourStr,
			currentUtil, premiumPct*100, priceAdj,
			expectedUtil, monthlyChange,
		)

		if g.Occurrences >= 12 {
			sug.Confidence = "high"
		} else if g.Occurrences >= 6 {
			sug.Confidence = "medium"
		} else {
			sug.Confidence = "low"
		}
	}

	return &sug
}

func getVenueHourlyPrices(db *gorm.DB) map[uint]float64 {
	prices := make(map[uint]float64)
	type row struct {
		ID          uint
		HourlyPrice float64
	}
	var rows []row
	db.Table("venues").Select("id, hourly_price").Scan(&rows)
	for _, r := range rows {
		prices[r.ID] = r.HourlyPrice
	}
	return prices
}
