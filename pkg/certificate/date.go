package certificate

import (
	"regexp"
	"strconv"
	"time"
)

var chineseDate = regexp.MustCompile(`^([0-9]{4})\s*年\s*([1-9]|1[0-2])\s*月\s*([1-9]|[12][0-9]|3[01])\s*日$`)
var isoCalendarDate = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}|[0-9]{8})$`)

// calendarDate 只解释完整的日历日期；其他 GB/T 7408 表达不猜测为无效日期。
func calendarDate(value string) (time.Time, bool, bool) {
	for _, layout := range []string{"2006-01-02", "20060102"} {
		if len(value) == len(layout) && isoCalendarDate.MatchString(value) {
			date, err := time.Parse(layout, value)
			return date, true, err == nil && date.Year() > 0
		}
	}
	if parts := chineseDate.FindStringSubmatch(value); parts != nil {
		year, _ := strconv.Atoi(parts[1])
		month, _ := strconv.Atoi(parts[2])
		day, _ := strconv.Atoi(parts[3])
		date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		return date, true, year > 0 && int(date.Month()) == month && date.Day() == day
	}
	return time.Time{}, false, false
}

func (c *metadataCheck) checkDates() {
	dates := make(map[string]time.Time)
	for _, name := range []string{"certificateIssuedDate", "certificateEffectiveDate", "certificateExpiringDate"} {
		value := c.values[name]
		if value == "" || (name == "certificateExpiringDate" && value == "长期") {
			continue
		}
		date, supported, valid := calendarDate(value)
		switch {
		case !supported:
			c.add(name, "date_unchecked", "GB/T 36903—2018 §5.2.7、§5.2.12—§5.2.13", "日期不是当前可解释的完整日历日期，需按 GB/T 7408 或行业规范复核")
			c.issues[len(c.issues)-1].Warning = true
		case !valid:
			c.add(name, "date_invalid", "GB/T 36903—2018 §5.2.7、§5.2.12—§5.2.13", "日历日期无效")
		default:
			dates[name] = date
		}
	}
	start, hasStart := dates["certificateEffectiveDate"]
	end, hasEnd := dates["certificateExpiringDate"]
	if hasStart && hasEnd && end.Before(start) {
		c.add("certificateExpiringDate", "date_order", "GB/T 36903—2018 §5.2.12—§5.2.13", "有效期截止日期早于起始日期")
	}
}
