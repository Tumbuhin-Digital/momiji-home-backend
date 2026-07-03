package warehouse

type WarehouseDTO struct {
	Code                   string  `json:"code"`
	Name                   string  `json:"name"`
	Phone                  string  `json:"phone"`
	Address1               string  `json:"address1"`
	City                   string  `json:"city"`
	State                  string  `json:"state"`
	Zip                    string  `json:"zip"`
	Country                string  `json:"country"`
	ShipstationWarehouseID *string `json:"shipstation_warehouse_id,omitempty"`
	GroundServiceCode      *string `json:"ground_service_code,omitempty"`
	IsDefault              bool    `json:"is_default"`
}

type WarehouseListResponse struct {
	Warehouses []WarehouseDTO `json:"warehouses"`
}

type UpdateWarehouseRequest struct {
	Name                   string  `json:"name"`
	Phone                  string  `json:"phone"`
	Address1               string  `json:"address1"`
	City                   string  `json:"city"`
	State                  string  `json:"state"`
	Zip                    string  `json:"zip"`
	Country                string  `json:"country"`
	ShipstationWarehouseID *string `json:"shipstation_warehouse_id"`
	GroundServiceCode      *string `json:"ground_service_code"`
}

func toDTO(w Warehouse) WarehouseDTO {
	return WarehouseDTO{
		Code:                   w.Code,
		Name:                   w.Name,
		Phone:                  w.Phone,
		Address1:               w.Address1,
		City:                   w.City,
		State:                  w.State,
		Zip:                    w.Zip,
		Country:                w.Country,
		ShipstationWarehouseID: w.ShipstationWarehouseID,
		GroundServiceCode:      w.GroundServiceCode,
		IsDefault:              w.IsDefault,
	}
}
