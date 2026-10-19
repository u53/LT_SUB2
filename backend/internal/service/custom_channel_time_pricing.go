package service

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

var channelTimePricingLocations sync.Map

// parsedChannelTimePeriod 是解析后的时段，包含倍率与可选绝对价。
type parsedChannelTimePeriod struct {
	start           int
	end             int
	multiplier      float64
	inputPrice      *float64
	outputPrice     *float64
	cacheWritePrice *float64
	cacheReadPrice  *float64
}

// hasAbsolutePrice 报告该时段是否配置了绝对价。
// 绝对价优先于倍率：上游给出独立的峰谷价格时，倍率语义不再适用。
func (p parsedChannelTimePeriod) hasAbsolutePrice() bool {
	return p.inputPrice != nil || p.outputPrice != nil ||
		p.cacheWritePrice != nil || p.cacheReadPrice != nil
}

// TimeTier 是某一时刻命中的时段定价结果。
type TimeTier struct {
	Multiplier      float64
	InputPrice      *float64
	OutputPrice     *float64
	CacheWritePrice *float64
	CacheReadPrice  *float64
}

// IsNeutral 报告该时段是否不改变价格（未命中任何时段）。
func (t TimeTier) IsNeutral() bool {
	return t.Multiplier == 1 && t.InputPrice == nil && t.OutputPrice == nil &&
		t.CacheWritePrice == nil && t.CacheReadPrice == nil
}

// validateChannelTimePricing 校验分时配置。nil 或空 periods 表示未启用。
func validateChannelTimePricing(config *ChannelTimePricing) error {
	if config == nil || len(config.Periods) == 0 {
		return nil
	}
	if _, err := loadChannelTimePricingLocation(config.Timezone); err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	if _, err := parseHolidayDates(config.Holidays); err != nil {
		return err
	}
	_, err := parseChannelTimePeriods(config.Periods)
	return err
}

// MultiplierAt 返回 at 对应的分时倍率。无配置或脏配置均安全降级为 1。
// 配置了绝对价的时段返回 1（绝对价由 TimeTierAt 应用）。
func (config *ChannelTimePricing) MultiplierAt(at time.Time) float64 {
	tier := config.TimeTierAt(at)
	if tier.InputPrice != nil || tier.OutputPrice != nil || tier.CacheWritePrice != nil || tier.CacheReadPrice != nil {
		return 1.0
	}
	return tier.Multiplier
}

// TimeTierAt 返回 at 对应的时段定价。无配置、脏配置或未命中时段时返回中性值。
//
// 节假日（Holidays）命中当日视为空闲时段：不套用任何 Period，直接按基础价。
// 这与上游「法定节假日全天低谷」的口径一致，且不需要为节假日单独配一条 00:00-24:00 时段。
func (config *ChannelTimePricing) TimeTierAt(at time.Time) TimeTier {
	neutral := TimeTier{Multiplier: 1.0}
	if config == nil || len(config.Periods) == 0 || at.IsZero() {
		return neutral
	}
	if err := validateChannelTimePricing(config); err != nil {
		return neutral
	}
	location, err := loadChannelTimePricingLocation(config.Timezone)
	if err != nil {
		return neutral
	}
	periods, err := parseChannelTimePeriods(config.Periods)
	if err != nil {
		return neutral
	}
	holidays, err := parseHolidayDates(config.Holidays)
	if err != nil {
		return neutral
	}

	local := at.In(location)
	if config.WeekdaysOnly && (local.Weekday() == time.Saturday || local.Weekday() == time.Sunday) {
		return neutral
	}
	if _, isHoliday := holidays[local.Format("2006-01-02")]; isHoliday {
		return neutral
	}
	second := local.Hour()*60*60 + local.Minute()*60 + local.Second()
	for _, period := range periods {
		if second >= period.start && second < period.end {
			return TimeTier{
				Multiplier:      period.multiplier,
				InputPrice:      period.inputPrice,
				OutputPrice:     period.outputPrice,
				CacheWritePrice: period.cacheWritePrice,
				CacheReadPrice:  period.cacheReadPrice,
			}
		}
	}
	return neutral
}

// parseHolidayDates 解析 YYYY-MM-DD 日期列表，返回集合供 O(1) 查询。
func parseHolidayDates(dates []string) (map[string]struct{}, error) {
	if len(dates) == 0 {
		return nil, nil
	}
	set := make(map[string]struct{}, len(dates))
	for _, raw := range dates {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil || parsed.Format("2006-01-02") != value {
			return nil, fmt.Errorf("holiday %q must use YYYY-MM-DD format", raw)
		}
		set[value] = struct{}{}
	}
	return set, nil
}

func loadChannelTimePricingLocation(name string) (*time.Location, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("timezone is required")
	}
	if name == "Local" {
		return nil, fmt.Errorf("local is not a supported timezone")
	}
	if cached, ok := channelTimePricingLocations.Load(name); ok {
		location, valid := cached.(*time.Location)
		if valid && location != nil {
			return location, nil
		}
		channelTimePricingLocations.Delete(name)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	actual, _ := channelTimePricingLocations.LoadOrStore(name, location)
	actualLocation, ok := actual.(*time.Location)
	if !ok || actualLocation == nil {
		return nil, fmt.Errorf("invalid cached timezone %q", name)
	}
	return actualLocation, nil
}

func parseChannelTime(value string, end bool) (int, error) {
	if end && (value == "00:00" || value == "00:00:00") {
		return 24 * 60 * 60, nil
	}
	layout := "15:04:05"
	if len(value) == len("15:04") {
		layout = "15:04"
	}
	parsed, err := time.Parse(layout, value)
	if err != nil || parsed.Format(layout) != value {
		return 0, fmt.Errorf("time %q must use HH:mm or HH:mm:ss format", value)
	}
	return parsed.Hour()*60*60 + parsed.Minute()*60 + parsed.Second(), nil
}

func parseChannelTimePeriods(periods []ChannelTimePricingPeriod) ([]parsedChannelTimePeriod, error) {
	parsed := make([]parsedChannelTimePeriod, 0, len(periods))
	for _, period := range periods {
		if err := validateTimePricingAbsolutePrices(period); err != nil {
			return nil, err
		}
		// 配置了绝对价的时段不需要倍率：绝对价直接替换单价。
		// 未配置绝对价时倍率必填，保持历史语义与校验强度。
		if !hasTimePricingAbsolutePrices(period) {
			if math.IsNaN(period.Multiplier) || math.IsInf(period.Multiplier, 0) || period.Multiplier <= 0 {
				return nil, fmt.Errorf("multiplier must be finite and greater than 0")
			}
			if period.Multiplier < 0.01 {
				return nil, fmt.Errorf("multiplier must be at least 0.01")
			}
			scaled := period.Multiplier * 100
			if math.IsNaN(scaled) || math.IsInf(scaled, 0) {
				return nil, fmt.Errorf("multiplier must remain finite when scaled")
			}
			if math.Abs(scaled-math.Round(scaled)) > 1e-9 {
				return nil, fmt.Errorf("multiplier must have at most two decimal places")
			}
		}

		start, err := parseChannelTime(period.StartTime, false)
		if err != nil {
			return nil, err
		}
		end, err := parseChannelTime(period.EndTime, true)
		if err != nil {
			return nil, err
		}
		if period.StartTime == period.EndTime || start >= end {
			return nil, fmt.Errorf("start time must be before end time")
		}
		parsed = append(parsed, parsedChannelTimePeriod{
			start:           start,
			end:             end,
			multiplier:      period.Multiplier,
			inputPrice:      period.InputPrice,
			outputPrice:     period.OutputPrice,
			cacheWritePrice: period.CacheWritePrice,
			cacheReadPrice:  period.CacheReadPrice,
		})
	}

	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].start < parsed[j].start
	})
	for i := 1; i < len(parsed); i++ {
		if parsed[i].start < parsed[i-1].end {
			return nil, fmt.Errorf("time pricing periods overlap")
		}
	}
	return parsed, nil
}

func hasTimePricingAbsolutePrices(period ChannelTimePricingPeriod) bool {
	return period.InputPrice != nil || period.OutputPrice != nil ||
		period.CacheWritePrice != nil || period.CacheReadPrice != nil
}

func validateTimePricingAbsolutePrices(period ChannelTimePricingPeriod) error {
	for name, price := range map[string]*float64{
		"input_price":       period.InputPrice,
		"output_price":      period.OutputPrice,
		"cache_write_price": period.CacheWritePrice,
		"cache_read_price":  period.CacheReadPrice,
	} {
		if price == nil {
			continue
		}
		if math.IsNaN(*price) || math.IsInf(*price, 0) || *price < 0 {
			return fmt.Errorf("%s must be finite and non-negative", name)
		}
	}
	return nil
}

// applyTimeTier 把时段定价应用到单价上。
// 绝对价优先：配置了绝对价的字段直接替换，未配置的字段沿用基础价（再乘倍率）。
// 该函数返回新对象，不修改入参（入参可能是共享的 fallbackPrices 指针）。
func applyTimeTier(pricing *ModelPricing, tier TimeTier) *ModelPricing {
	if pricing == nil || tier.IsNeutral() {
		return pricing
	}
	cloned := *pricing
	if tier.InputPrice != nil {
		cloned.InputPricePerToken = *tier.InputPrice
		cloned.InputPricePerTokenPriority = *tier.InputPrice
	}
	if tier.OutputPrice != nil {
		cloned.OutputPricePerToken = *tier.OutputPrice
		cloned.OutputPricePerTokenPriority = *tier.OutputPrice
	}
	if tier.CacheWritePrice != nil {
		cloned.CacheCreationPricePerToken = *tier.CacheWritePrice
		cloned.CacheCreationPricePerTokenPriority = *tier.CacheWritePrice
		cloned.CacheCreation5mPrice = *tier.CacheWritePrice
		cloned.CacheCreation1hPrice = *tier.CacheWritePrice
		cloned.CacheCreationPriceExplicit = true
	}
	if tier.CacheReadPrice != nil {
		cloned.CacheReadPricePerToken = *tier.CacheReadPrice
		cloned.CacheReadPricePerTokenPriority = *tier.CacheReadPrice
	}
	if tier.Multiplier != 1 {
		cloned.InputPricePerToken *= tier.Multiplier
		cloned.InputPricePerTokenPriority *= tier.Multiplier
		cloned.ImageInputPricePerToken *= tier.Multiplier
		cloned.ImageCacheReadPricePerToken *= tier.Multiplier
		cloned.OutputPricePerToken *= tier.Multiplier
		cloned.OutputPricePerTokenPriority *= tier.Multiplier
		cloned.CacheCreationPricePerToken *= tier.Multiplier
		cloned.CacheCreationPricePerTokenPriority *= tier.Multiplier
		cloned.CacheReadPricePerToken *= tier.Multiplier
		cloned.CacheReadPricePerTokenPriority *= tier.Multiplier
		cloned.CacheCreation5mPrice *= tier.Multiplier
		cloned.CacheCreation1hPrice *= tier.Multiplier
		cloned.ImageOutputPricePerToken *= tier.Multiplier
	}
	return &cloned
}
