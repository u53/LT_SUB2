//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 币种折算：价格条目自带币种，计费前折成记账币种
// ---------------------------------------------------------------------------

func TestPricingCurrencyConverter_SameCurrencyIsIdentity(t *testing.T) {
	c := NewPricingCurrencyConverter(config.BillingConfig{AccountingCurrency: "CNY", USDToAccountingRate: 7.1})
	pricing := &ModelPricing{InputPricePerToken: 1e-6, Currency: "CNY"}

	converted, ok := c.ConvertPricing(pricing)
	require.True(t, ok)
	require.Same(t, pricing, converted, "同币种必须原样返回，不引入浮点误差")
}

func TestPricingCurrencyConverter_EmptyCurrencyMeansAccounting(t *testing.T) {
	c := NewPricingCurrencyConverter(config.BillingConfig{AccountingCurrency: "CNY", USDToAccountingRate: 7.1})
	pricing := &ModelPricing{InputPricePerToken: 1e-6}

	converted, ok := c.ConvertPricing(pricing)
	require.True(t, ok)
	require.InDelta(t, 1e-6, converted.InputPricePerToken, 1e-15,
		"未标注币种的历史条目语义不变，不得被折算")
}

func TestPricingCurrencyConverter_ConvertsUSDToCNY(t *testing.T) {
	c := NewPricingCurrencyConverter(config.BillingConfig{AccountingCurrency: "CNY", USDToAccountingRate: 7.1})
	pricing := &ModelPricing{
		InputPricePerToken:             1e-6,
		OutputPricePerToken:            2e-6,
		CacheReadPricePerToken:         1e-7,
		CacheCreation5mPrice:           1.25e-6,
		CacheCreation1hPrice:           2e-6,
		ImageInputPricePerToken:        3e-6,
		ImageOutputPricePerToken:       4e-6,
		InputPricePerTokenPriority:     5e-6,
		OutputPricePerTokenPriority:    6e-6,
		CacheReadPricePerTokenPriority: 7e-6,
		Currency:                       "USD",
	}

	converted, ok := c.ConvertPricing(pricing)
	require.True(t, ok)
	require.InDelta(t, 1e-6*7.1, converted.InputPricePerToken, 1e-15)
	require.InDelta(t, 2e-6*7.1, converted.OutputPricePerToken, 1e-15)
	require.InDelta(t, 1e-7*7.1, converted.CacheReadPricePerToken, 1e-15)
	require.InDelta(t, 1.25e-6*7.1, converted.CacheCreation5mPrice, 1e-15)
	require.InDelta(t, 2e-6*7.1, converted.CacheCreation1hPrice, 1e-15)
	require.InDelta(t, 3e-6*7.1, converted.ImageInputPricePerToken, 1e-15)
	require.InDelta(t, 4e-6*7.1, converted.ImageOutputPricePerToken, 1e-15)
	require.InDelta(t, 5e-6*7.1, converted.InputPricePerTokenPriority, 1e-15)
	require.InDelta(t, 6e-6*7.1, converted.OutputPricePerTokenPriority, 1e-15)
	require.InDelta(t, 7e-6*7.1, converted.CacheReadPricePerTokenPriority, 1e-15)
	require.Empty(t, converted.Currency, "折算后必须清空来源币种，避免二次折算")
	require.InDelta(t, 1e-6, pricing.InputPricePerToken, 1e-15, "不得修改入参")
}

func TestPricingCurrencyConverter_MissingRateKeepsPriceAndReportsFalse(t *testing.T) {
	c := NewPricingCurrencyConverter(config.BillingConfig{AccountingCurrency: "CNY"})
	pricing := &ModelPricing{InputPricePerToken: 1e-6, Currency: "EUR"}

	converted, ok := c.ConvertPricing(pricing)
	require.False(t, ok, "缺汇率必须显式报告，不得静默按 1:1 折算")
	require.InDelta(t, 1e-6, converted.InputPricePerToken, 1e-15)
}

func TestPricingCurrencyConverter_CurrencyRatesOverrideUSD(t *testing.T) {
	c := NewPricingCurrencyConverter(config.BillingConfig{
		AccountingCurrency:  "CNY",
		USDToAccountingRate: 7.0,
		CurrencyRates:       map[string]float64{"USD": 7.2},
	})
	rate, ok := c.Rate("USD")
	require.True(t, ok)
	require.InDelta(t, 7.2, rate, 1e-12, "currency_rates 显式配置优先于 usd_to_accounting_rate")
}

// ---------------------------------------------------------------------------
// 时段定价：绝对价优先于倍率，节假日与周末按基础价
// ---------------------------------------------------------------------------

func TestTimeTierAt_AbsolutePriceOverridesMultiplier(t *testing.T) {
	inputPrice := 2e-6
	outputPrice := 8e-6
	config := &ChannelTimePricing{
		Timezone: "Asia/Shanghai",
		Periods: []ChannelTimePricingPeriod{{
			StartTime:   "09:00",
			EndTime:     "12:00",
			InputPrice:  &inputPrice,
			OutputPrice: &outputPrice,
		}},
	}

	tier := config.TimeTierAt(time.Date(2026, 8, 17, 1, 30, 0, 0, time.UTC)) // 北京 09:30
	require.NotNil(t, tier.InputPrice)
	require.InDelta(t, inputPrice, *tier.InputPrice, 1e-15)
	require.InDelta(t, outputPrice, *tier.OutputPrice, 1e-15)
	require.Equal(t, 1.0, config.MultiplierAt(time.Date(2026, 8, 17, 1, 30, 0, 0, time.UTC)),
		"绝对价时段不应同时报告倍率")

	outside := config.TimeTierAt(time.Date(2026, 8, 17, 5, 0, 0, 0, time.UTC)) // 北京 13:00
	require.True(t, outside.IsNeutral())
}

func TestTimeTierAt_HolidayFallsBackToBasePrice(t *testing.T) {
	config := &ChannelTimePricing{
		Timezone:     "Asia/Shanghai",
		WeekdaysOnly: true,
		Holidays:     []string{"2026-10-01"},
		Periods: []ChannelTimePricingPeriod{{
			StartTime: "09:00", EndTime: "12:00", Multiplier: 2,
		}},
	}

	// 2026-10-01 是周四（工作日），但为国庆假期 → 应回落基础价。
	holiday := time.Date(2026, 10, 1, 1, 30, 0, 0, time.UTC) // 北京 09:30
	require.Equal(t, 1.0, config.MultiplierAt(holiday))

	// 同一时刻的前一天（工作日、非节假日）→ 高峰倍率生效。
	workday := time.Date(2026, 9, 30, 1, 30, 0, 0, time.UTC) // 北京 09:30
	require.Equal(t, 2.0, config.MultiplierAt(workday))
}

func TestTimeTierAt_InvalidHolidayDegradesSafely(t *testing.T) {
	config := &ChannelTimePricing{
		Timezone: "Asia/Shanghai",
		Holidays: []string{"2026/10/01"},
		Periods: []ChannelTimePricingPeriod{{
			StartTime: "09:00", EndTime: "12:00", Multiplier: 2,
		}},
	}
	tier := config.TimeTierAt(time.Date(2026, 10, 1, 1, 30, 0, 0, time.UTC))
	require.True(t, tier.IsNeutral(), "脏节假日配置应降级为不改价，而不是误判高峰")
}

func TestApplyTimeTier_DoesNotMutateSharedPricing(t *testing.T) {
	base := &ModelPricing{InputPricePerToken: 1e-6, OutputPricePerToken: 2e-6}
	tier := TimeTier{Multiplier: 2}

	got := applyTimeTier(base, tier)
	require.InDelta(t, 2e-6, got.InputPricePerToken, 1e-15)
	require.InDelta(t, 4e-6, got.OutputPricePerToken, 1e-15)
	require.InDelta(t, 1e-6, base.InputPricePerToken, 1e-15, "不得污染共享 fallbackPrices 指针")
}

func TestApplyTimeTier_NeutralReturnsSamePointer(t *testing.T) {
	base := &ModelPricing{InputPricePerToken: 1e-6}
	require.Same(t, base, applyTimeTier(base, TimeTier{Multiplier: 1}))
}

// ---------------------------------------------------------------------------
// DeepSeek 官方峰谷：人民币口径 + 法定节假日
// ---------------------------------------------------------------------------

func TestDeepseekPeakMultiplierAt_BeijingHoursAndHolidays(t *testing.T) {
	// 2026-08-24 周一
	at := func(hour, min int) time.Time { return time.Date(2026, 8, 24, hour, min, 0, 0, time.UTC) }

	require.Equal(t, 2.0, deepseekPeakMultiplierAt(at(1, 0)), "北京 09:00 进入高峰")
	require.Equal(t, 2.0, deepseekPeakMultiplierAt(at(3, 59)), "北京 11:59 仍在高峰")
	require.Equal(t, 1.0, deepseekPeakMultiplierAt(at(4, 0)), "北京 12:00 离开高峰")
	require.Equal(t, 2.0, deepseekPeakMultiplierAt(at(6, 0)), "北京 14:00 进入高峰")
	require.Equal(t, 1.0, deepseekPeakMultiplierAt(at(10, 0)), "北京 18:00 离开高峰")

	// 国庆假期（2026-10-01 周四）全天低谷，即使落在工作日高峰时段。
	require.Equal(t, 1.0, deepseekPeakMultiplierAt(time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)))
}

func TestDeepseekFallbackPricing_UsesCNYRatesWhenAccountingInCNY(t *testing.T) {
	pro, flash := deepseekFallbackPricing(CurrencyCNY)

	// ¥1 / MTok 输入、¥4 / MTok 输出、¥0.02 / MTok 缓存命中
	require.InDelta(t, 1.0/1e6, flash.InputPricePerToken, 1e-18)
	require.InDelta(t, 4.0/1e6, flash.OutputPricePerToken, 1e-18)
	require.InDelta(t, 0.02/1e6, flash.CacheReadPricePerToken, 1e-18)
	require.Equal(t, CurrencyCNY, flash.Currency)
	require.NotNil(t, flash.TimePricing)

	// ¥4.5 / MTok 输入、¥13.5 / MTok 输出、¥0.15 / MTok 缓存命中
	require.InDelta(t, 4.5/1e6, pro.InputPricePerToken, 1e-18)
	require.InDelta(t, 13.5/1e6, pro.OutputPricePerToken, 1e-18)
	require.InDelta(t, 0.15/1e6, pro.CacheReadPricePerToken, 1e-18)
	require.Equal(t, CurrencyCNY, pro.Currency)
}

func TestDeepseekFallbackPricing_UsesUSDRatesOtherwise(t *testing.T) {
	_, flash := deepseekFallbackPricing(CurrencyUSD)
	require.InDelta(t, 1.5e-7, flash.InputPricePerToken, 1e-18)
	require.Empty(t, flash.Currency, "美元记账时币种留空，表示与记账币种相同")
}

func TestCalculateCostUnified_DeepseekCNYChargesOffPeakAndPeak(t *testing.T) {
	bs := NewBillingService(&config.Config{Billing: config.BillingConfig{AccountingCurrency: CurrencyCNY}}, nil)
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	// ¥1/MTok × 1000 + ¥4/MTok × 500 + ¥0.02/MTok × 1000
	offPeakTotal := 1000*1.0/1e6 + 500*4.0/1e6 + 1000*0.02/1e6

	offPeak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), // 北京 20:00 低谷
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, offPeak.TotalCost, 1e-12)

	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), // 北京 10:00 高峰
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal*2, peak.TotalCost, 1e-12)
}

func TestCalculateCostUnified_DeepseekHolidayChargesOffPeak(t *testing.T) {
	bs := NewBillingService(&config.Config{Billing: config.BillingConfig{AccountingCurrency: CurrencyCNY}}, nil)
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000}
	offPeakTotal := 1000 * 1.0 / 1e6

	// 2026-10-01 国庆（周四）北京 10:00，本应是高峰时段，但节假日全天低谷。
	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, cost.TotalCost, 1e-12)
}

func TestCalculateCostUnified_DeepseekCustomGroupPricingNotOverridden(t *testing.T) {
	// 分组自定义价不再被官方默认峰谷叠加：运营者配置的价格必须原样生效。
	bs := NewBillingService(&config.Config{Billing: config.BillingConfig{AccountingCurrency: CurrencyCNY}}, nil)
	resolver := NewModelPricingResolver(nil, bs)

	inputPrice := 1e-6
	group := &Group{
		ID: 1, Name: "ds-group", Platform: PlatformDeepseek, Status: StatusActive,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"deepseek-flash"}, BillingMode: BillingModeToken,
			InputPrice: &inputPrice,
		}},
	}

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-flash", Group: group,
		Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), // 高峰时段
	})
	require.NoError(t, err)
	require.InDelta(t, 1000*inputPrice, cost.TotalCost, 1e-12,
		"分组自定义价不应被官方峰谷倍率改写")
}

// ---------------------------------------------------------------------------
// 配置校验（校验方法属于 config 包，此处只覆盖服务侧可观察行为）
// ---------------------------------------------------------------------------

func TestBillingConfig_AccountingCurrencyOrDefault(t *testing.T) {
	require.Equal(t, CurrencyUSD, config.BillingConfig{}.AccountingCurrencyOrDefault())
	require.Equal(t, "CNY", config.BillingConfig{AccountingCurrency: "cny"}.AccountingCurrencyOrDefault())
}

func TestBillingConfig_RateToAccounting(t *testing.T) {
	cfg := config.BillingConfig{AccountingCurrency: "CNY", USDToAccountingRate: 7.1}

	rate, ok := cfg.RateToAccounting("USD")
	require.True(t, ok)
	require.InDelta(t, 7.1, rate, 1e-12)

	rate, ok = cfg.RateToAccounting("CNY")
	require.True(t, ok)
	require.InDelta(t, 1.0, rate, 1e-12)

	_, ok = cfg.RateToAccounting("EUR")
	require.False(t, ok, "未配置的币种必须报告缺汇率")
}
