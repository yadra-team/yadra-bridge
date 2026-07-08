package redact

import "regexp"

type Category string

const (
	CategoryAPIKey     Category = "api_key"
	CategoryCard       Category = "credit_card"
	CategoryIBAN       Category = "iban"
	CategoryEmail      Category = "email"
	CategoryPhone      Category = "phone"
	CategoryIP         Category = "ip"
	CategoryNationalID Category = "national_id"
)

type rule struct {
	category    Category
	re          *regexp.Regexp
	replacement string
	validate    func(string) bool
}

func defaultRules() []rule {
	return []rule{
		{
			category:    CategoryAPIKey,
			re:          regexp.MustCompile(`(?i)(?:sk-[a-z0-9]{20,}|ghp_[a-zA-Z0-9]{20,}|AKIA[0-9A-Z]{16}|eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)`),
			replacement: "[API_KEY]",
		},
		{
			category:    CategoryCard,
			re:          regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
			replacement: "[CREDIT_CARD]",
			validate:    luhnValid,
		},
		{
			category:    CategoryIBAN,
			re:          regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}\b`),
			replacement: "[IBAN]",
		},
		{
			category:    CategoryEmail,
			re:          regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`),
			replacement: "[EMAIL]",
		},
		{
			category:    CategoryPhone,
			re:          regexp.MustCompile(`(?:\+[1-9]\d{7,14}\b|(?:\+1[\s.-]?)?\(?\d{3}\)?[\s.-]\d{3}[\s.-]\d{4}\b)`),
			replacement: "[PHONE]",
		},
		{
			category:    CategoryIP,
			re:          regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b|\b(?:[0-9a-fA-F]{1,4}:){2,7}[0-9a-fA-F]{1,4}\b`),
			replacement: "[IP]",
		},
		{
			category:    CategoryNationalID,
			re:          regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
			replacement: "[NATIONAL_ID]",
		},
	}

}

func luhnValid(s string) bool {
	digits := make([]int, 0, len(s))
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := digits[i]
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

func phoneValid(s string) bool {
	digits := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	return digits >= 10 && digits <= 15
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}
