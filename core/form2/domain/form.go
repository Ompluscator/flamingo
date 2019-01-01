package domain

import "fmt"

type (
	// Form as struct for storing form processing results
	Form struct {
		// Data  the form Data Struct (Forms DTO)
		Data interface{}
		// ValidationInfo for the form
		ValidationInfo ValidationInfo
		// submitted  flag if form was submitted and this is the result page
		submitted bool
		// validationRules contains map with validation rules for all validatable fields
		validationRules map[string][]ValidationRule
	}

	FormError string
)

// NewForm returns new instance of Form struct
func NewForm(submitted bool, validationRules map[string][]ValidationRule) Form {
	return Form{
		submitted:       submitted,
		validationRules: validationRules,
	}
}

// IsValidAndSubmitted defines if form data is valid and form is submitted
func (f Form) IsValidAndSubmitted() bool {
	return f.IsValid() && f.IsSubmitted()
}

// IsValid defines if form data is valid
func (f Form) IsValid() bool {
	return f.ValidationInfo.IsValid()
}

// IsSubmitted defines if form is submitted
func (f Form) IsSubmitted() bool {
	return f.submitted
}

// HasErrorsForField method which defines if there is any field validations error for specific field
func (f Form) HasErrorForField(name string) bool {
	return f.ValidationInfo.HasErrorsForField(name)
}

// HasAnyFieldErrors method which defines if there is any field validations error for any field
func (f Form) HasAnyFieldErrors() bool {
	return f.ValidationInfo.HasAnyFieldErrors()
}

// HasGeneralErrors method which defines if there is any general validations error
func (f Form) HasGeneralErrors() bool {
	return f.ValidationInfo.HasGeneralErrors()
}

// GetFieldErrors method which returns list of all general validation errors for specific field
func (f Form) GetErrorsForField(name string) []Error {
	return f.ValidationInfo.GetErrorsForField(name)
}

//GetValidationRulesForField adds option to extract validation rules for desired field in templates
func (f Form) GetValidationRulesForField(name string) []ValidationRule {
	return f.validationRules[name]
}

func NewFormError(details string) FormError {
	return FormError(details)
}

func NewFormErrorf(details string, args ...interface{}) FormError {
	return FormError(fmt.Sprintf(details, args...))
}

func (e FormError) Error() string {
	return fmt.Sprintf("FormError: %s", e)
}