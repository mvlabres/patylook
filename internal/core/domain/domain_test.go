package domain

import (
	"errors"
	"testing"
	"time"
)

func TestParseMoney(t *testing.T) {
	tests := []struct {
		in      string
		want    Money
		wantErr bool
	}{
		{"12", 1200, false},
		{"12.5", 1250, false},
		{"12,50", 1250, false},
		{"1.234,56", 123456, false},
		{"1234.56", 123456, false},
		{"0,05", 5, false},
		{",5", 50, false},
		{"-3,10", -310, false},
		{" 7,00 ", 700, false},
		{"", 0, true},
		{"abc", 0, true},
		{"1,234", 0, true},
		{"1.2.3", 0, true},
		{"12,5x", 0, true},
		{"-", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseMoney(tt.in)
		if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
			t.Errorf("ParseMoney(%q) = %d, %v; want %d (err=%v)", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestMoneyFormatting(t *testing.T) {
	if got := Money(123456).String(); got != "R$ 1.234,56" {
		t.Errorf("String = %q", got)
	}
	if got := Money(-5).String(); got != "-R$ 0,05" {
		t.Errorf("String negative = %q", got)
	}
	if got := Money(123456).Decimal(); got != "1234.56" {
		t.Errorf("Decimal = %q", got)
	}
	if got := Money(123456).Input(); got != "1234,56" {
		t.Errorf("Input = %q", got)
	}
	if got := Percent(1250).Decimal(); got != "12.50" {
		t.Errorf("Percent.Decimal = %q", got)
	}
}

func TestMoneyPercentRounding(t *testing.T) {
	tests := []struct {
		amount Money
		pct    Percent
		want   Money
	}{
		{10000, 1000, 1000}, // 10% of 100,00
		{1, 5000, 1},        // 0,01 * 50% = 0,005 -> rounds half up
		{999, 1000, 100},    // 9,99 * 10% = 0,999 -> 1,00
		{0, 5000, 0},
		{-1, 5000, -1}, // symmetric rounding
	}
	for _, tt := range tests {
		if got := tt.amount.Percent(tt.pct); got != tt.want {
			t.Errorf("%d.Percent(%d) = %d, want %d", tt.amount, tt.pct, got, tt.want)
		}
	}
}

func TestValidCPF(t *testing.T) {
	valid := []string{"52998224725", "11144477735"}
	invalid := []string{"", "123", "11111111111", "52998224726", "5299822472a", "529982247250"}
	for _, c := range valid {
		if !ValidCPF(c) {
			t.Errorf("ValidCPF(%q) = false", c)
		}
	}
	for _, c := range invalid {
		if ValidCPF(c) {
			t.Errorf("ValidCPF(%q) = true", c)
		}
	}
}

func TestPricebookFinalPrice(t *testing.T) {
	p := &Pricebook{
		ProductCost:   Money(5000),
		PackagingCost: Money(500),
		LogisticsCost: Money(300),
		OtherCosts:    Money(200),
		MarkupPercent: Percent(5000), // 50%
		ValidFrom:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.TotalCost() != 6000 || p.FinalPrice != 9000 {
		t.Errorf("cost=%d final=%d", p.TotalCost(), p.FinalPrice)
	}

	to := p.ValidFrom.AddDate(0, 0, -1)
	p.ValidTo = &to
	if err := p.Validate(); err == nil {
		t.Error("expected error when valid_to is before valid_from")
	}
}

func TestOrderCalculate(t *testing.T) {
	order := func() *Order {
		return &Order{
			CustomerID: 1, PaymentTypeID: 1,
			Items: []OrderItem{
				{ProductID: 1, Quantity: 2, UnitPrice: 5000, Discount: 1000}, // 90,00
				{ProductID: 2, Quantity: 1, UnitPrice: 2000},                 // 20,00
			},
			Discount: 1000,
		}
	}

	o := order()
	if err := o.Calculate(Percent(500)); err != nil { // 5% surcharge
		t.Fatal(err)
	}
	// subtotal 110,00; minus 10,00 = 100,00; +5% = 5,00
	if o.Subtotal != 11000 || o.PaymentAddition != 500 || o.Total != 10500 {
		t.Errorf("subtotal=%d addition=%d total=%d", o.Subtotal, o.PaymentAddition, o.Total)
	}

	cases := map[string]func(*Order){
		"no items":          func(o *Order) { o.Items = nil },
		"duplicate product": func(o *Order) { o.Items[1].ProductID = 1 },
		"zero quantity":     func(o *Order) { o.Items[0].Quantity = 0 },
		"item discount>gross": func(o *Order) {
			o.Items[1].Discount = 2001
		},
		"order discount>subtotal": func(o *Order) { o.Discount = 11001 },
		"no customer":             func(o *Order) { o.CustomerID = 0 },
	}
	for name, mutate := range cases {
		o := order()
		mutate(o)
		err := o.Calculate(0)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: expected ValidationError, got %v", name, err)
		}
	}
}

func TestOrderStatusTransitions(t *testing.T) {
	tests := []struct {
		from, to OrderStatus
		ok       bool
	}{
		{OrderOpen, OrderCompleted, true},
		{OrderOpen, OrderCanceled, true},
		{OrderCompleted, OrderCanceled, true},
		{OrderCompleted, OrderOpen, false},
		{OrderCanceled, OrderOpen, false},
		{OrderCanceled, OrderCompleted, false},
		{OrderOpen, OrderOpen, false},
	}
	for _, tt := range tests {
		if got := tt.from.CanTransitionTo(tt.to); got != tt.ok {
			t.Errorf("%s -> %s = %v, want %v", tt.from, tt.to, got, tt.ok)
		}
	}
}

func TestUserValidate(t *testing.T) {
	ok := &User{Username: "maria.silva", Name: "Maria", Email: "maria@example.com", CategoryID: 1}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid user rejected: %v", err)
	}
	for name, u := range map[string]*User{
		"short username":   {Username: "ab", Name: "x", CategoryID: 1},
		"long username":    {Username: "abcdefghijklmnopq", Name: "x", CategoryID: 1},
		"bad chars":        {Username: "a b c", Name: "x", CategoryID: 1},
		"no name":          {Username: "abc", CategoryID: 1},
		"bad email":        {Username: "abc", Name: "x", Email: "nope", CategoryID: 1},
		"missing category": {Username: "abc", Name: "x"},
	} {
		if err := u.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestPasswordPolicy(t *testing.T) {
	if err := ValidatePassword("password", "short"); err == nil {
		t.Error("short password accepted")
	}
	if err := ValidatePassword("password", "long-enough-password"); err != nil {
		t.Errorf("valid password rejected: %v", err)
	}
	long := make([]byte, MaxPasswordLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidatePassword("password", string(long)); err == nil {
		t.Error("password above bcrypt limit accepted")
	}
}

func TestConflictErrorMatchesSentinel(t *testing.T) {
	var err error = &ConflictError{Field: "sku"}
	if !errors.Is(err, ErrConflict) {
		t.Error("ConflictError should match ErrConflict")
	}
}

func TestImageFolder(t *testing.T) {
	tests := []struct {
		sku, want string
		ok        bool
	}{
		{"CAM-1", "CAM-1", true},
		{"cam 1", "cam-1", true},
		{`A/B\C`, "A-B-C", true},
		{"  X.Y_Z  ", "X.Y_Z", true},
		{"Camiseta Ação", "Camiseta-A-o", true},
		{"../etc", "etc", true},
		{"..", "", false},
		{"///", "", false},
		{"", "", false},
		{"...", "", false},
	}
	for _, tt := range tests {
		got, err := ImageFolder(tt.sku)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("ImageFolder(%q) = %q, %v; want %q (ok=%v)", tt.sku, got, err, tt.want, tt.ok)
		}
	}
}

func TestImagePaths(t *testing.T) {
	p := ImagePath("CAM-1", MainImageName)
	if p != "images/products/CAM-1/principal.jpg" {
		t.Errorf("ImagePath = %q", p)
	}
	if got := ImageFolderFromPath(p); got != "CAM-1" {
		t.Errorf("ImageFolderFromPath = %q", got)
	}
	img := ProductImage{FileName: MainImageName, Path: p}
	if !img.IsMain() || img.URL() != "/images/products/CAM-1/principal.jpg" {
		t.Errorf("image helpers: %v %q", img.IsMain(), img.URL())
	}
	if (ProductImage{FileName: "imagem-01.jpg"}).IsMain() {
		t.Error("carousel image must not be main")
	}
}
