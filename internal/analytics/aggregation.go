package analytics

import (
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
)

type AggRequest struct {
	Metrics     []string `json:"metrics"`
	Dimensions  []string `json:"dimensions"`
	StartDate   string   `json:"start_date"`
	EndDate     string   `json:"end_date"`
	Granularity string   `json:"granularity"`
	VenueID     *uint    `json:"venue_id"`
	SportType   string   `json:"sport_type"`
	Compare     string   `json:"compare"`
}

type AggResult struct {
	Items []map[string]interface{} `json:"items"`
}

func Aggregate(db *gorm.DB, req AggRequest) (*AggResult, error) {
	if len(req.Dimensions) == 0 {
		req.Dimensions = []string{"venue_id"}
	}
	if req.Granularity == "" {
		req.Granularity = "day"
	}
	selectParts, groupParts, err := buildSelectGroup(req)
	if err != nil {
		return nil, err
	}

	where := "date BETWEEN ? AND ?"
	args := []interface{}{req.StartDate, req.EndDate}
	if req.VenueID != nil {
		where += " AND venue_id = ?"
		args = append(args, *req.VenueID)
	}
	if req.SportType != "" {
		where += " AND sport_type = ?"
		args = append(args, req.SportType)
	}

	sqlStr := fmt.Sprintf(
		"SELECT %s FROM stat_hourlies WHERE %s GROUP BY %s ORDER BY %s",
		strings.Join(selectParts, ", "), where,
		strings.Join(groupParts, ", "), strings.Join(groupParts, ", "),
	)

	results, err := queryToMaps(db, sqlStr, args...)
	if err != nil {
		return nil, err
	}

	if req.Compare != "" {
		compStart, compEnd, err := getComparePeriod(req.StartDate, req.EndDate, req.Compare)
		if err != nil {
			return nil, err
		}
		compSelect, compGroup, _ := buildCompSelectGroup(req)
		compWhere := "date BETWEEN ? AND ?"
		compArgs := []interface{}{compStart, compEnd}
		if req.VenueID != nil {
			compWhere += " AND venue_id = ?"
			compArgs = append(compArgs, *req.VenueID)
		}
		if req.SportType != "" {
			compWhere += " AND sport_type = ?"
			compArgs = append(compArgs, req.SportType)
		}
		compSQL := fmt.Sprintf(
			"SELECT %s FROM stat_hourlies WHERE %s GROUP BY %s",
			strings.Join(compSelect, ", "), compWhere,
			strings.Join(compGroup, ", "),
		)
		compResults, err := queryToMaps(db, compSQL, compArgs...)
		if err != nil {
			return nil, err
		}
		results = mergeComparison(results, compResults, req)
	}

	return &AggResult{Items: results}, nil
}

func buildSelectGroup(req AggRequest) ([]string, []string, error) {
	var sel, grp []string
	dimSet := make(map[string]bool)
	for _, d := range req.Dimensions {
		dimSet[d] = true
	}

	if dimSet["venue_id"] {
		sel = append(sel, "venue_id", "venue_name")
		grp = append(grp, "venue_id", "venue_name")
	}
	if dimSet["sport_type"] {
		sel = append(sel, "sport_type")
		grp = append(grp, "sport_type")
	}

	switch req.Granularity {
	case "hour":
		sel = append(sel, "date", "hour")
		grp = append(grp, "date", "hour")
	case "day":
		sel = append(sel, "date")
		grp = append(grp, "date")
	case "week":
		sel = append(sel, "YEARWEEK(date, 1) AS period")
		grp = append(grp, "YEARWEEK(date, 1)")
	case "month":
		sel = append(sel, "DATE_FORMAT(date, '%Y-%m') AS period")
		grp = append(grp, "DATE_FORMAT(date, '%Y-%m')")
	default:
		return nil, nil, fmt.Errorf("unsupported granularity: %s", req.Granularity)
	}

	if dimSet["weekday"] {
		sel = append(sel, "weekday")
		grp = append(grp, "weekday")
	}
	if dimSet["hour"] && req.Granularity != "hour" {
		sel = append(sel, "hour")
		grp = append(grp, "hour")
	}

	sel = append(sel,
		"SUM(total_slots) AS total_slots",
		"SUM(booked_slots) AS booked_slots",
		"ROUND(SUM(booked_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS utilization_rate",
		"ROUND(SUM(revenue), 2) AS revenue",
		"ROUND(SUM(revenue) / NULLIF(SUM(booked_slots), 0), 2) AS avg_price",
		"ROUND(SUM(cancelled_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS cancel_rate",
	)

	return sel, grp, nil
}

func buildCompSelectGroup(req AggRequest) ([]string, []string, error) {
	var sel, grp []string
	dimSet := make(map[string]bool)
	for _, d := range req.Dimensions {
		dimSet[d] = true
	}

	if dimSet["venue_id"] {
		sel = append(sel, "venue_id", "venue_name")
		grp = append(grp, "venue_id", "venue_name")
	}
	if dimSet["sport_type"] {
		sel = append(sel, "sport_type")
		grp = append(grp, "sport_type")
	}

	sel = append(sel,
		"SUM(total_slots) AS prev_total_slots",
		"SUM(booked_slots) AS prev_booked_slots",
		"ROUND(SUM(booked_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS prev_utilization_rate",
		"ROUND(SUM(revenue), 2) AS prev_revenue",
		"ROUND(SUM(revenue) / NULLIF(SUM(booked_slots), 0), 2) AS prev_avg_price",
		"ROUND(SUM(cancelled_slots) * 100.0 / NULLIF(SUM(total_slots), 0), 2) AS prev_cancel_rate",
	)

	return sel, grp, nil
}

func getComparePeriod(start, end, compare string) (string, string, error) {
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return "", "", err
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return "", "", err
	}
	dur := e.Sub(s)
	switch compare {
	case "mom":
		s = s.AddDate(0, -1, 0)
		e = s.Add(dur)
	case "yoy":
		s = s.AddDate(-1, 0, 0)
		e = s.Add(dur)
	case "wow":
		s = s.AddDate(0, 0, -7)
		e = s.Add(dur)
	default:
		return "", "", fmt.Errorf("unsupported compare: %s", compare)
	}
	return s.Format("2006-01-02"), e.Format("2006-01-02"), nil
}

func mergeComparison(current, prev []map[string]interface{}, req AggRequest) []map[string]interface{} {
	prevParsed := make(map[string]map[string]interface{})
	for _, p := range prev {
		key := compDimKey(p)
		prevParsed[key] = p
	}

	for _, row := range current {
		key := compDimKey(row)
		if pv, ok := prevParsed[key]; ok {
			row["comparison"] = map[string]interface{}{
				"utilization_rate_prev":   pv["prev_utilization_rate"],
				"utilization_rate_change": round2(toFloat(row["utilization_rate"]) - toFloat(pv["prev_utilization_rate"])),
				"revenue_prev":            pv["prev_revenue"],
				"revenue_change":          round2(toFloat(row["revenue"]) - toFloat(pv["prev_revenue"])),
				"avg_price_prev":          pv["prev_avg_price"],
				"cancel_rate_prev":        pv["prev_cancel_rate"],
				"cancel_rate_change":      round2(toFloat(row["cancel_rate"]) - toFloat(pv["prev_cancel_rate"])),
			}
		}
	}

	return current
}

func compDimKey(row map[string]interface{}) string {
	var parts []string
	if v, ok := row["venue_id"]; ok {
		parts = append(parts, fmt.Sprintf("v:%v", v))
	}
	if v, ok := row["sport_type"]; ok {
		parts = append(parts, fmt.Sprintf("s:%v", v))
	}
	return strings.Join(parts, "|")
}

func queryToMaps(db *gorm.DB, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := db.Raw(sql, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			var v interface{}
			ptrs[i] = &v
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{})
		for i, col := range cols {
			val := *(ptrs[i].(*interface{}))
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}
	return results, nil
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

func toFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}

var WeekdayNames = map[int]string{
	1: "周一", 2: "周二", 3: "周三", 4: "周四", 5: "周五", 6: "周六", 7: "周日",
}

func toInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case uint:
		return int(val)
	case float64:
		return int(val)
	case string:
		var i int
		fmt.Sscanf(val, "%d", &i)
		return i
	default:
		return 0
	}
}
