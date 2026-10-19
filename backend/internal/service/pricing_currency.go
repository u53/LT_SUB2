package service

import (
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// CurrencyUSD 是价格目录（LiteLLM / 内置兜底价）的计价币种。
// 目录里的价格全部以美元官网价维护，记账币种非 USD 时由 PricingCurrencyConverter 折算。
const CurrencyUSD = "USD"

// CurrencyCNY 是人民币记账时国产模型的录入币种。
const CurrencyCNY = "CNY"

// PricingCurrencyConverter 把价格条目从其录入币种折成记账币种。
//
// 设计约束：
//   - 价格条目自带币种：国产模型直接录入人民币官网价，海外模型保留美元官网价；
//   - 折算只作用于价格，不作用于已发生的金额。余额、配额、用量成本始终是记账币种；
//   - 同币种直接返回原值，不引入任何浮点误差。
type PricingCurrencyConverter struct {
	accountingCurrency string
	rates              map[string]float64
}

// NewPricingCurrencyConverter 按配置构造转换器。
// accountingCurrency 为空时按 USD 处理，保持既有部署行为。
func NewPricingCurrencyConverter(cfg config.BillingConfig) *PricingCurrencyConverter {
	accounting := strings.ToUpper(strings.TrimSpace(cfg.AccountingCurrency))
	if accounting == "" {
		accounting = CurrencyUSD
	}
	rates := make(map[string]float64, len(cfg.CurrencyRates)+1)
	for code, rate := range cfg.CurrencyRates {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			continue
		}
		rates[code] = rate
	}
	if cfg.USDToAccountingRate > 0 && !math.IsNaN(cfg.USDToAccountingRate) && !math.IsInf(cfg.USDToAccountingRate, 0) {
		if _, exists := rates[CurrencyUSD]; !exists {
			rates[CurrencyUSD] = cfg.USDToAccountingRate
		}
	}
	return &PricingCurrencyConverter{accountingCurrency: accounting, rates: rates}
}

// AccountingCurrency 返回记账币种。
func (c *PricingCurrencyConverter) AccountingCurrency() string {
	if c == nil || c.accountingCurrency == "" {
		return CurrencyUSD
	}
	return c.accountingCurrency
}

// Rate 返回 1 单位 from 折合记账币种的系数。
// 第二个返回值为 false 表示缺少该币种的汇率。
func (c *PricingCurrencyConverter) Rate(from string) (float64, bool) {
	from = normalizePricingCurrency(from)
	accounting := c.AccountingCurrency()
	if from == "" || from == accounting {
		return 1, true
	}
	if c == nil {
		return 0, false
	}
	rate, ok := c.rates[from]
	if !ok || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	return rate, true
}

// ConvertPricing 把价格条目折成记账币种。
//
// 条目币种为空时视为记账币种（历史数据与未标注币种的渠道/分组价），
// 这样升级后既有配置的数值语义不变。
// 缺少汇率时返回原价与 false，调用方决定是否告警；不静默按 1:1 折算。
func (c *PricingCurrencyConverter) ConvertPricing(pricing *ModelPricing) (*ModelPricing, bool) {
	if pricing == nil {
		return nil, true
	}
	source := normalizePricingCurrency(pricing.Currency)
	if source == "" || source == c.AccountingCurrency() {
		return pricing, true
	}
	rate, ok := c.Rate(source)
	if !ok || rate == 1 {
		return pricing, ok
	}
	cloned := *pricing
	cloned.InputPricePerToken *= rate
	cloned.InputPricePerTokenPriority *= rate
	cloned.ImageInputPricePerToken *= rate
	cloned.ImageCacheReadPricePerToken *= rate
	cloned.OutputPricePerToken *= rate
	cloned.OutputPricePerTokenPriority *= rate
	cloned.CacheCreationPricePerToken *= rate
	cloned.CacheCreationPricePerTokenPriority *= rate
	cloned.CacheReadPricePerToken *= rate
	cloned.CacheReadPricePerTokenPriority *= rate
	cloned.CacheCreation5mPrice *= rate
	cloned.CacheCreation1hPrice *= rate
	cloned.ImageOutputPricePerToken *= rate
	// 折算后价格已是记账币种，清空来源币种，避免二次折算。
	cloned.Currency = ""
	return &cloned, true
}

// normalizePricingCurrency 规范化币种代码；空值保持空串（表示记账币种）。
func normalizePricingCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}
