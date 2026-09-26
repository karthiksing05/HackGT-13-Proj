package contract

import "errors"

type Preferences struct {
	Ratings                   Ratings     `json:"ratings"`
	Company                   Company     `json:"company"`
	Pace                      Pace        `json:"pace"`
	Spend                     SpendTier   `json:"spend"`
	Flexibility               Flexibility `json:"flexibility"`
	SplitStyle                SplitStyle  `json:"split_style"`
	PreferFree                bool        `json:"prefer_free"`
	Answers                   Answers     `json:"answers"`
	InstantCheckout           bool        `json:"instant_checkout"`
	InstantCheckoutLimitCents int         `json:"instant_checkout_limit_cents"`
}

// DefaultPreferences is GET /me/preferences before the first save.
func DefaultPreferences() Preferences {
	return Preferences{
		Ratings:                   Ratings{},
		Company:                   CompanySmallGroup,
		Pace:                      PaceBalanced,
		Spend:                     SpendUnder15,
		Flexibility:               FlexBitOverOK,
		SplitStyle:                SplitEqually,
		PreferFree:                true,
		Answers:                   Answers{},
		InstantCheckout:           false,
		InstantCheckoutLimitCents: 5000,
	}
}

// Validate checks enums, rating keys and values and the checkout limit; it
// also fills empty enums with their defaults so a partial PUT still saves.
func (p *Preferences) Validate() error {
	def := DefaultPreferences()
	if p.Company == "" {
		p.Company = def.Company
	}
	if p.Pace == "" {
		p.Pace = def.Pace
	}
	if p.Spend == "" {
		p.Spend = def.Spend
	}
	if p.Flexibility == "" {
		p.Flexibility = def.Flexibility
	}
	if p.SplitStyle == "" {
		p.SplitStyle = def.SplitStyle
	}
	if p.Ratings == nil {
		p.Ratings = Ratings{}
	}
	if p.Answers == nil {
		p.Answers = Answers{}
	}
	if !p.Company.Valid() || !p.Pace.Valid() || !p.Spend.Valid() || !p.Flexibility.Valid() || !p.SplitStyle.Valid() {
		return errors.New("Pick one of the offered options for company, pace, spend, flexibility and split style.")
	}
	if !p.Ratings.Validate() {
		return errors.New("Ratings are 1 to 5 for the trip types offered.")
	}
	if p.InstantCheckoutLimitCents < 0 {
		return errors.New("The instant checkout limit can't be negative.")
	}
	return nil
}

type TasteBar struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type TasteProfile struct {
	Bars []TasteBar `json:"bars"`
}

type Integration struct {
	Provider  CalendarProvider `json:"provider"`
	Connected bool             `json:"connected"`
}

type PaymentMethod struct {
	ID        string `json:"id"`
	Brand     string `json:"brand"`
	Last4     string `json:"last4"`
	IsDefault bool   `json:"is_default"`
}

// AddPaymentMethod is POST /me/payment-methods ({token} from a payment SDK).
type AddPaymentMethod struct {
	Token string `json:"token"`
}
