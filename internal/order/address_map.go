package order

import "github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"

func addressToDTO(addr *customer.Address) *AddressDTO {
	if addr == nil {
		return nil
	}

	firstName := ""
	lastName := ""
	company := ""
	address2 := ""
	phone := ""
	if addr.FirstName != nil {
		firstName = *addr.FirstName
	}
	if addr.LastName != nil {
		lastName = *addr.LastName
	}
	if addr.Company != nil {
		company = *addr.Company
	}
	if addr.Address2 != nil {
		address2 = *addr.Address2
	}
	if addr.Phone != nil {
		phone = *addr.Phone
	}

	return &AddressDTO{
		FirstName: firstName,
		LastName:  lastName,
		Company:   company,
		Address1:  addr.Address1,
		Address2:  address2,
		City:      addr.City,
		Province:  addr.Province,
		Country:   addr.Country,
		Zip:       addr.Zip,
		Phone:     phone,
	}
}
