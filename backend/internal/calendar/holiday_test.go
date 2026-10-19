package calendar

import (
	"testing"
	"time"
)

func TestCNHolidays_LoadsOfficialData(t *testing.T) {
	set, err := CNHolidays()
	if err != nil {
		t.Fatalf("load holidays: %v", err)
	}
	if len(set) == 0 {
		t.Fatal("embedded holiday data is empty")
	}

	// 2026 年国庆：10 月 1 日至 7 日
	for _, date := range []string{"2026-10-01", "2026-10-02", "2026-10-07"} {
		if _, ok := set[date]; !ok {
			t.Errorf("%s should be a holiday", date)
		}
	}
	// 2026 年春节：2 月 15 日至 23 日
	if _, ok := set["2026-02-17"]; !ok {
		t.Error("2026-02-17 (Spring Festival) should be a holiday")
	}
	// 普通工作日不是节假日
	if _, ok := set["2026-10-08"]; ok {
		t.Error("2026-10-08 should not be a holiday")
	}
}

func TestIsCNHoliday(t *testing.T) {
	// 按日期自身时区取年月日：北京时间 2026-10-01 00:30 = UTC 2026-09-30 16:30，
	// 用北京时间解释时应判定为节假日。
	beijing := time.FixedZone("Asia/Shanghai", 8*3600)
	if !IsCNHoliday(time.Date(2026, 10, 1, 0, 30, 0, 0, beijing)) {
		t.Error("2026-10-01 in Beijing time should be a holiday")
	}
	if IsCNHoliday(time.Date(2026, 9, 30, 12, 0, 0, 0, beijing)) {
		t.Error("2026-09-30 should not be a holiday")
	}
}

func TestCNHolidaysForYear(t *testing.T) {
	dates, err := CNHolidaysForYear(2026)
	if err != nil {
		t.Fatalf("holidays for 2026: %v", err)
	}
	if len(dates) != 33 {
		t.Fatalf("2026 should have 33 holiday dates, got %d", len(dates))
	}
	if dates[0] != "2026-01-01" {
		t.Errorf("expected sorted dates starting at 2026-01-01, got %s", dates[0])
	}
}

func TestHolidayDataYears(t *testing.T) {
	years, err := HolidayDataYears()
	if err != nil {
		t.Fatalf("holiday data years: %v", err)
	}
	if len(years) == 0 {
		t.Fatal("expected at least one year of holiday data")
	}
	for i := 1; i < len(years); i++ {
		if years[i] <= years[i-1] {
			t.Fatalf("years must be sorted ascending, got %v", years)
		}
	}
}

func TestParseHolidayFile_RejectsInvalidDate(t *testing.T) {
	_, err := parseHolidayFile([]byte(`{"years":{"2026":{"holidays":["2026/10/01"]}}}`))
	if err == nil {
		t.Fatal("expected invalid date to be rejected")
	}
}

func TestParseHolidayFile_RejectsBadJSON(t *testing.T) {
	if _, err := parseHolidayFile([]byte(`{`)); err == nil {
		t.Fatal("expected malformed JSON to be rejected")
	}
}
