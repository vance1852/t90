package analytics

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type RevenueRequest struct {
	StartDate   string `json:"start_date" form:"start_date"`
	EndDate     string `json:"end_date" form:"end_date"`
	Granularity string `json:"granularity" form:"granularity"`
	VenueID     *uint  `json:"venue_id" form:"venue_id"`
	SportType   string `json:"sport_type" form:"sport_type"`
	GroupBy     string `json:"group_by" form:"group_by"`
}

type RevenueResult struct {
	Items []RevenueRow `json:"items"`
	Total RevenueTotal `json:"total"`
}

type RevenueRow struct {
	Period    string  `json:"period,omitempty"`
	VenueID   uint    `json:"venue_id,omitempty"`
	VenueName string  `json:"venue_name,omitempty"`
	SportType string  `json:"sport_type,omitempty"`
	VenueFee  float64 `json:"venue_fee"`
	Package   float64 `json:"package"`
	Equipment float64 `json:"equipment"`
	Course    float64 `json:"course"`
	Refund    float64 `json:"refund"`
	Net       float64 `json:"net"`
}

type RevenueTotal struct {
	VenueFee  float64 `json:"venue_fee"`
	Package   float64 `json:"package"`
	Equipment float64 `json:"equipment"`
	Course    float64 `json:"course"`
	Refund    float64 `json:"refund"`
	Net       float64 `json:"net"`
}

func RevenueReport(db *gorm.DB, req RevenueRequest) (*RevenueResult, error) {
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

	var selParts, grpParts []string
	switch req.Granularity {
	case "month":
		selParts = append(selParts, "DATE_FORMAT(date, '%Y-%m') AS period")
		grpParts = append(grpParts, "DATE_FORMAT(date, '%Y-%m')")
	case "week":
		selParts = append(selParts, "YEARWEEK(date, 1) AS period")
		grpParts = append(grpParts, "YEARWEEK(date, 1)")
	case "day":
		selParts = append(selParts, "date AS period")
		grpParts = append(grpParts, "date")
	default:
		selParts = append(selParts, "DATE_FORMAT(date, '%Y-%m') AS period")
		grpParts = append(grpParts, "DATE_FORMAT(date, '%Y-%m')")
	}

	if req.GroupBy == "venue" {
		selParts = append(selParts, "venue_id", "venue_name", "sport_type")
		grpParts = append(grpParts, "venue_id", "venue_name", "sport_type")
	}

	selParts = append(selParts,
		"ROUND(SUM(CASE WHEN type = 'venue_fee' THEN amount ELSE 0 END), 2) AS venue_fee",
		"ROUND(SUM(CASE WHEN type = 'package' THEN amount ELSE 0 END), 2) AS package",
		"ROUND(SUM(CASE WHEN type = 'equipment' THEN amount ELSE 0 END), 2) AS equipment",
		"ROUND(SUM(CASE WHEN type = 'course' THEN amount ELSE 0 END), 2) AS course",
		"ROUND(SUM(CASE WHEN type = 'refund' THEN ABS(amount) ELSE 0 END), 2) AS refund",
		"ROUND(SUM(CASE WHEN type <> 'refund' THEN amount ELSE -ABS(amount) END), 2) AS net",
	)

	sqlStr := fmt.Sprintf(
		"SELECT %s FROM revenue_items WHERE %s GROUP BY %s ORDER BY %s",
		strings.Join(selParts, ", "), where,
		strings.Join(grpParts, ", "), strings.Join(grpParts, ", "),
	)

	raw, err := queryToMaps(db, sqlStr, args...)
	if err != nil {
		return nil, err
	}

	var items []RevenueRow
	var total RevenueTotal
	for _, r := range raw {
		row := RevenueRow{
			VenueFee:  toFloat(r["venue_fee"]),
			Package:   toFloat(r["package"]),
			Equipment: toFloat(r["equipment"]),
			Course:    toFloat(r["course"]),
			Refund:    toFloat(r["refund"]),
			Net:       toFloat(r["net"]),
		}
		if v, ok := r["period"]; ok {
			row.Period = fmt.Sprintf("%v", v)
		}
		if v, ok := r["venue_id"]; ok {
			row.VenueID = uint(toInt(v))
		}
		if v, ok := r["venue_name"]; ok {
			row.VenueName = fmt.Sprintf("%v", v)
		}
		if v, ok := r["sport_type"]; ok {
			row.SportType = fmt.Sprintf("%v", v)
		}
		items = append(items, row)
		total.VenueFee += row.VenueFee
		total.Package += row.Package
		total.Equipment += row.Equipment
		total.Course += row.Course
		total.Refund += row.Refund
		total.Net += row.Net
	}

	total.VenueFee = round2(total.VenueFee)
	total.Package = round2(total.Package)
	total.Equipment = round2(total.Equipment)
	total.Course = round2(total.Course)
	total.Refund = round2(total.Refund)
	total.Net = round2(total.Net)

	return &RevenueResult{Items: items, Total: total}, nil
}

func RevenueExport(db *gorm.DB, req RevenueRequest) ([][]string, error) {
	result, err := RevenueReport(db, req)
	if err != nil {
		return nil, err
	}

	header := []string{"期间"}
	if req.GroupBy == "venue" {
		header = append(header, "场馆ID", "场馆名称", "项目类型")
	}
	header = append(header, "场地费", "套餐核销", "器材租赁", "课程收入", "退款冲销", "净营收")

	var rows [][]string
	rows = append(rows, header)
	for _, item := range result.Items {
		row := []string{item.Period}
		if req.GroupBy == "venue" {
			row = append(row,
				fmt.Sprintf("%d", item.VenueID),
				item.VenueName,
				item.SportType,
			)
		}
		row = append(row,
			fmt.Sprintf("%.2f", item.VenueFee),
			fmt.Sprintf("%.2f", item.Package),
			fmt.Sprintf("%.2f", item.Equipment),
			fmt.Sprintf("%.2f", item.Course),
			fmt.Sprintf("%.2f", item.Refund),
			fmt.Sprintf("%.2f", item.Net),
		)
		rows = append(rows, row)
	}

	summary := []string{"合计"}
	if req.GroupBy == "venue" {
		summary = append(summary, "", "", "")
	}
	summary = append(summary,
		fmt.Sprintf("%.2f", result.Total.VenueFee),
		fmt.Sprintf("%.2f", result.Total.Package),
		fmt.Sprintf("%.2f", result.Total.Equipment),
		fmt.Sprintf("%.2f", result.Total.Course),
		fmt.Sprintf("%.2f", result.Total.Refund),
		fmt.Sprintf("%.2f", result.Total.Net),
	)
	rows = append(rows, summary)

	return rows, nil
}
