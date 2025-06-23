package utils

import (
	"math"
	"strconv"
)

func FloatToInt64(num float64) int64 {
	// Round to the nearest even integer
	rounded := math.RoundToEven(num)

	// Convert to int64
	int64Value := int64(rounded)

	return int64Value
}

func BytesToKb(num string) int64 {
	//transform string to float64
	numFloat64, err := strconv.ParseFloat(num, 64)
	if err != nil {
		Logline("Error converting string to float64:", err)
		return 0
	}

	// se divide entre 125 porque se recibe en bytes y se guarda en kbps, asi que seria lo mismo que multiplicar por 8 y luego dividir entre 1000
	// 8/1000 es igual ha 1/125
	numKilobits := numFloat64 / 125

	//transform float64 to int64 and return
	return FloatToInt64(numKilobits)
}

func StringToInt64(num string) int64 {
	// Convert string to int64
	int64Value, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		Logline("Error converting string to int64:", err)
		return 0
	}

	return int64Value
}

func Int64ToString(num int64) string {
	// Convert int64 to string
	return strconv.FormatInt(num, 10)
}
