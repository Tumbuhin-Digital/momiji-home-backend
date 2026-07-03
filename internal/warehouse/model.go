package warehouse

import "time"

const (
	CodeEast = "east"
	CodeWest = "west"
)

type Warehouse struct {
	ID                     string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Code                   string    `gorm:"column:code;uniqueIndex;not null"`
	Name                   string    `gorm:"column:name;not null"`
	Phone                  string    `gorm:"column:phone;not null"`
	Address1               string    `gorm:"column:address1;not null"`
	City                   string    `gorm:"column:city;not null"`
	State                  string    `gorm:"column:state;not null"`
	Zip                    string    `gorm:"column:zip;not null"`
	Country                string    `gorm:"column:country;not null"`
	ShipstationWarehouseID *string   `gorm:"column:shipstation_warehouse_id"`
	GroundServiceCode      *string   `gorm:"column:ground_service_code"`
	IsDefault              bool      `gorm:"column:is_default;not null"`
	CreatedAt              time.Time `gorm:"column:created_at"`
	UpdatedAt              time.Time `gorm:"column:updated_at"`
}

func (Warehouse) TableName() string {
	return "warehouses"
}

// Origin is the resolved ship-from address used for rate calculation.
type Origin struct {
	Code              string
	Name              string
	Phone             string
	Address1          string
	City              string
	State             string
	Zip               string
	Country           string
	GroundServiceCode string
}

func (w Warehouse) ToOrigin(globalGroundCode string) Origin {
	ground := globalGroundCode
	if w.GroundServiceCode != nil && *w.GroundServiceCode != "" {
		ground = *w.GroundServiceCode
	}
	country := w.Country
	if country == "" {
		country = "US"
	}
	return Origin{
		Code:              w.Code,
		Name:              w.Name,
		Phone:             w.Phone,
		Address1:          w.Address1,
		City:              w.City,
		State:             w.State,
		Zip:               w.Zip,
		Country:           country,
		GroundServiceCode: ground,
	}
}
