// Package calendar 提供计费时段判定需要的日历数据。
//
// 中国法定节假日无法从公历推算：春节、清明、端午、中秋按农历或节气，
// 且每年由国务院办公厅发布《部分节假日安排的通知》确定具体放假与调休日期。
// 因此这里采用数据驱动：随二进制嵌入一份日期表，每年国务院发布次年安排后
// 追加对应年份即可，不需要改动计费逻辑。
//
// 数据边界：只收录「放假」日期。调休上班的周末仍按周末处理，因为上游
// （如 DeepSeek）的峰谷口径是「周一至周五（不含法定节假日）」，周末即使
// 调休上班也不属于高峰。
package calendar

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

//go:embed cn_holidays.json
var cnHolidayJSON []byte

// holidayDateLayout 是日期表的键格式。
const holidayDateLayout = "2006-01-02"

type holidayYear struct {
	Source   string   `json:"source"`
	Holidays []string `json:"holidays"`
}

type holidayFile struct {
	Version  int                    `json:"version"`
	Timezone string                 `json:"timezone"`
	Years    map[string]holidayYear `json:"years"`
}

var (
	holidayOnce sync.Once
	holidaySet  map[string]struct{}
	holidayErr  error
)

// CNHolidays 返回内置的中国法定节假日集合，键为 YYYY-MM-DD。
// 返回的 map 只读，调用方不得修改。
func CNHolidays() (map[string]struct{}, error) {
	holidayOnce.Do(func() {
		holidaySet, holidayErr = parseHolidayFile(cnHolidayJSON)
	})
	return holidaySet, holidayErr
}

// IsCNHoliday 判断指定日期（按 date 自身时区取年月日）是否为法定节假日。
// 数据缺失或解析失败时返回 false：节假日只是「更便宜」的时段，
// 缺失时按工作日处理属于保守方向，不会少收。
func IsCNHoliday(date time.Time) bool {
	set, err := CNHolidays()
	if err != nil || set == nil {
		return false
	}
	_, ok := set[date.Format(holidayDateLayout)]
	return ok
}

// CNHolidaysForYear 返回某年的节假日日期列表，按字典序排列。
func CNHolidaysForYear(year int) ([]string, error) {
	set, err := CNHolidays()
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%04d-", year)
	var dates []string
	for date := range set {
		if strings.HasPrefix(date, prefix) {
			dates = append(dates, date)
		}
	}
	sortStrings(dates)
	return dates, nil
}

// HolidayDataYears 返回内置数据覆盖的年份，用于启动自检与运维提示。
func HolidayDataYears() ([]int, error) {
	var file holidayFile
	if err := json.Unmarshal(cnHolidayJSON, &file); err != nil {
		return nil, fmt.Errorf("parse embedded holiday data: %w", err)
	}
	years := make([]int, 0, len(file.Years))
	for raw := range file.Years {
		var year int
		if _, err := fmt.Sscanf(raw, "%d", &year); err != nil {
			continue
		}
		years = append(years, year)
	}
	sortInts(years)
	return years, nil
}

func parseHolidayFile(raw []byte) (map[string]struct{}, error) {
	var file holidayFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse embedded holiday data: %w", err)
	}
	set := make(map[string]struct{}, len(file.Years)*16)
	for year, entry := range file.Years {
		for _, date := range entry.Holidays {
			if err := validateDate(date); err != nil {
				return nil, fmt.Errorf("holiday %q in year %s: %w", date, year, err)
			}
			set[date] = struct{}{}
		}
	}
	return set, nil
}

func validateDate(value string) error {
	parsed, err := time.Parse(holidayDateLayout, value)
	if err != nil || parsed.Format(holidayDateLayout) != value {
		return fmt.Errorf("must use YYYY-MM-DD")
	}
	return nil
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func sortInts(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
