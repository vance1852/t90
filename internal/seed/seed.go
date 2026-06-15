package seed

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"gorm.io/gorm"

	"venue-booking-admin/internal/auth"
	"venue-booking-admin/internal/models"
)

var surnames = []string{"陈", "周", "黄", "吴", "李", "张", "王", "刘", "赵", "林", "杨", "孙", "马", "朱", "胡", "郭", "何", "高", "罗", "郑"}
var givenNames = []string{"伟", "芳", "娜", "敏", "静", "杰", "强", "磊", "军", "丽", "艳", "勇", "涛", "明", "超", "洋", "娟", "鹏", "辉", "婷", "宇", "峰", "浩", "东", "燕", "红", "梅", "刚", "斌", "健"}

var hourBaseRate = map[int]float64{
	6: 0.15, 7: 0.20, 8: 0.30, 9: 0.38, 10: 0.42, 11: 0.48,
	12: 0.30, 13: 0.25, 14: 0.22, 15: 0.28, 16: 0.38, 17: 0.52,
	18: 0.72, 19: 0.78, 20: 0.72, 21: 0.45, 22: 0.15,
}

var weekdayMult = map[int]float64{
	1: 0.78, 2: 0.72, 3: 0.76, 4: 0.82, 5: 0.98,
	6: 1.30, 7: 1.12,
}

type venueProfile struct {
	hourMult  float64
	sportType string
}

var venueProfiles = map[string]venueProfile{
	"basketball":  {hourMult: 1.0, sportType: "basketball"},
	"swimming":    {hourMult: 1.08, sportType: "swimming"},
	"badminton":   {hourMult: 0.35, sportType: "badminton"},
	"football":    {hourMult: 0.92, sportType: "football"},
	"tennis":      {hourMult: 0.95, sportType: "tennis"},
	"table_tennis": {hourMult: 1.02, sportType: "table_tennis"},
}

type slotInfo struct {
	venueID     uint
	venueName   string
	sportType   string
	date        string
	hour        int
	weekday     int
	booked      bool
	cancelled   bool
	hourlyPrice float64
}

func Run(database *gorm.DB, adminUser, adminPass string) error {
	var count int64
	database.Model(&models.User{}).Where("username = ?", adminUser).Count(&count)
	if count == 0 {
		hash, err := auth.HashPassword(adminPass)
		if err != nil {
			return err
		}
		database.Create(&models.User{Username: adminUser, PasswordHash: hash, DisplayName: "平台管理员"})
		log.Println("已创建管理员账号")
	}

	var venueCount int64
	database.Model(&models.Venue{}).Count(&venueCount)
	if venueCount > 0 {
		return nil
	}

	venues := []models.Venue{
		{Name: "城北全民健身中心篮球馆", SportType: "basketball", Capacity: 200, HourlyPrice: 160, OpenHour: 8, CloseHour: 22, Status: "open"},
		{Name: "奥体中心游泳馆", SportType: "swimming", Capacity: 400, HourlyPrice: 80, OpenHour: 6, CloseHour: 21, Status: "open"},
		{Name: "市民广场羽毛球馆", SportType: "badminton", Capacity: 60, HourlyPrice: 50, OpenHour: 9, CloseHour: 22, Status: "maintenance"},
		{Name: "滨江足球公园", SportType: "football", Capacity: 500, HourlyPrice: 300, OpenHour: 8, CloseHour: 20, Status: "open"},
		{Name: "天河网球中心", SportType: "tennis", Capacity: 80, HourlyPrice: 120, OpenHour: 7, CloseHour: 22, Status: "open"},
		{Name: "龙湖乒乓球馆", SportType: "table_tennis", Capacity: 40, HourlyPrice: 40, OpenHour: 8, CloseHour: 22, Status: "open"},
	}
	if err := database.Create(&venues).Error; err != nil {
		return err
	}

	rng := rand.New(rand.NewSource(42))
	startDate := time.Date(2025, 12, 1, 0, 0, 0, 0, time.Local)
	endDate := time.Date(2026, 6, 14, 0, 0, 0, 0, time.Local)

	var slots []slotInfo
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		for _, v := range venues {
			profile := venueProfiles[v.SportType]
			wd := isoWeekday(d)
			for h := v.OpenHour; h < v.CloseHour; h++ {
				baseRate := hourBaseRate[h]
				if baseRate == 0 {
					baseRate = 0.2
				}
				prob := baseRate * weekdayMult[wd] * profile.hourMult
				prob = clamp(prob, 0, 0.95)

				isBooked := rng.Float64() < prob
				isCancelled := false
				if isBooked && rng.Float64() < 0.12 {
					isCancelled = true
				}

				slots = append(slots, slotInfo{
					venueID:     v.ID,
					venueName:   v.Name,
					sportType:   v.SportType,
					date:        d.Format("2006-01-02"),
					hour:        h,
					weekday:     wd,
					booked:      isBooked && !isCancelled,
					cancelled:   isCancelled,
					hourlyPrice: v.HourlyPrice,
				})
			}
		}
	}

	bookings := groupSlotsToBookings(slots, rng)
	if err := database.CreateInBatches(bookings, 500).Error; err != nil {
		return fmt.Errorf("create bookings: %w", err)
	}
	log.Printf("已生成 %d 条预订记录", len(bookings))

	revItems := generateRevenueItems(bookings, slots, rng)
	if err := database.CreateInBatches(revItems, 500).Error; err != nil {
		return fmt.Errorf("create revenue items: %w", err)
	}
	log.Printf("已生成 %d 条营收明细", len(revItems))

	stats := generateStatHourly(slots)
	if err := database.CreateInBatches(stats, 500).Error; err != nil {
		return fmt.Errorf("create stat hourly: %w", err)
	}
	log.Printf("已生成 %d 条时段统计", len(stats))

	log.Println("种子数据初始化完成")
	return nil
}

func isoWeekday(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func groupSlotsToBookings(slots []slotInfo, rng *rand.Rand) []models.Booking {
	type key struct {
		venueID uint
		date    string
		status  bool
	}
	grouped := make(map[key][]slotInfo)
	for _, s := range slots {
		if !s.booked && !s.cancelled {
			continue
		}
		k := key{venueID: s.venueID, date: s.date, status: s.cancelled}
		grouped[k] = append(grouped[k], s)
	}

	var bookings []models.Booking
	for k, groupSlots := range grouped {
		hourSet := make(map[int]bool)
		for _, s := range groupSlots {
			hourSet[s.hour] = true
		}

		var runs [][]int
		var current []int
		for h := 0; h <= 23; h++ {
			if hourSet[h] {
				current = append(current, h)
			} else {
				if len(current) >= 2 {
					runs = append(runs, append([]int{}, current...))
				}
				current = nil
			}
		}
		if len(current) >= 2 {
			runs = append(runs, current)
		}

		if len(runs) == 0 && len(groupSlots) > 0 {
			var hours []int
			for h := range hourSet {
				hours = append(hours, h)
			}
			if len(hours) > 0 {
				runs = append(runs, hours[:1])
			}
		}

		for _, run := range runs {
			startH := run[0]
			endH := run[len(run)-1] + 1

			surname := surnames[rng.Intn(len(surnames))]
			given := givenNames[rng.Intn(len(givenNames))]
			name := surname + given
			phone := fmt.Sprintf("1%d%08d", 3+rng.Intn(7), rng.Intn(100000000))

			price := groupSlots[0].hourlyPrice
			amount := price * float64(endH-startH)

			status := "booked"
			if k.status {
				status = "cancelled"
			}
			if !k.status && rng.Float64() < 0.15 {
				status = "completed"
			}

			bookings = append(bookings, models.Booking{
				VenueID:      k.venueID,
				CustomerName: name,
				Phone:        phone,
				BookDate:     k.date,
				StartHour:    startH,
				EndHour:      endH,
				Amount:       amount,
				Status:       status,
				CreatedAt:    time.Now(),
			})
		}
	}

	return bookings
}

func generateRevenueItems(bookings []models.Booking, slots []slotInfo, rng *rand.Rand) []models.RevenueItem {
	slotMap := make(map[string]slotInfo)
	for _, s := range slots {
		key := fmt.Sprintf("%d_%s_%d", s.venueID, s.date, s.hour)
		slotMap[key] = s
	}

	var items []models.RevenueItem
	for _, b := range bookings {
		if b.Status == "cancelled" {
			refundPerHour := 0.0
			for _, s := range slots {
				if s.venueID == b.VenueID {
					refundPerHour = s.hourlyPrice
					break
				}
			}
			refundAmt := refundPerHour * float64(b.EndHour-b.StartHour) * 0.8

			var vn, st string
			for _, s := range slots {
				if s.venueID == b.VenueID {
					vn = s.venueName
					st = s.sportType
					break
				}
			}

			for h := b.StartHour; h < b.EndHour; h++ {
				items = append(items, models.RevenueItem{
					BookingID: b.ID,
					VenueID:   b.VenueID,
					VenueName: vn,
					SportType: st,
					Type:      "refund",
					Amount:    -roundTo2(refundAmt / float64(b.EndHour-b.StartHour)),
					Date:      b.BookDate,
					Hour:      h,
					CreatedAt: time.Now(),
				})
			}
			continue
		}

		venueFeeRate := 0.70 + rng.Float64()*0.05 - 0.025
		packageRate := 0.12 + rng.Float64()*0.06
		equipmentRate := 0.08 + rng.Float64()*0.04
		courseRate := 1.0 - venueFeeRate - packageRate - equipmentRate
		if courseRate < 0 {
			courseRate = 0
		}

		var vn, st string
		hourlyPrice := 0.0
		for _, s := range slots {
			if s.venueID == b.VenueID {
				vn = s.venueName
				st = s.sportType
				hourlyPrice = s.hourlyPrice
				break
			}
		}

		totalPerHour := hourlyPrice * 1.18
		hours := b.EndHour - b.StartHour

		types := []struct {
			t string
			r float64
		}{
			{"venue_fee", venueFeeRate},
			{"package", packageRate},
			{"equipment", equipmentRate},
			{"course", courseRate},
		}

		for _, tp := range types {
			amt := roundTo2(totalPerHour * tp.r * float64(hours))
			if amt <= 0 {
				continue
			}
			perHour := roundTo2(amt / float64(hours))
			for h := b.StartHour; h < b.EndHour; h++ {
				items = append(items, models.RevenueItem{
					BookingID: b.ID,
					VenueID:   b.VenueID,
					VenueName: vn,
					SportType: st,
					Type:      tp.t,
					Amount:    perHour,
					Date:      b.BookDate,
					Hour:      h,
					CreatedAt: time.Now(),
				})
			}
		}
	}

	return items
}

func generateStatHourly(slots []slotInfo) []models.StatHourly {
	var stats []models.StatHourly
	for _, s := range slots {
		var booked, cancelled int
		var revenue float64
		if s.booked {
			booked = 1
			revenue = s.hourlyPrice
		}
		if s.cancelled {
			cancelled = 1
		}

		stats = append(stats, models.StatHourly{
			VenueID:        s.venueID,
			VenueName:      s.venueName,
			SportType:      s.sportType,
			Date:           s.date,
			Hour:           s.hour,
			Weekday:        s.weekday,
			TotalSlots:     1,
			BookedSlots:    booked,
			Revenue:        revenue,
			CancelledSlots: cancelled,
		})
	}
	return stats
}

func roundTo2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
