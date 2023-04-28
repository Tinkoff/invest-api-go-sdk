package investapi

import "math"

// ToFloat - converting value to float64
func (q *Quotation) ToFloat() float64 {
	if q != nil {
		return float64(q.Units) + float64(q.Nano)*math.Pow10(-9)
	}
	return float64(0)
}

// ToFloat - get value as float64 number
func (mv *MoneyValue) ToFloat() float64 {
	if mv != nil {
		return float64(mv.Units) + float64(mv.Nano)*math.Pow10(-9)
	}
	return float64(0)
}
