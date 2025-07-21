package client

import (
	"fmt"
)

type APIError struct {
	StatusCode int
	Name       string
	Message    string
}

func (ae *APIError) Error() string {
	if ae.StatusCode > 0 {
		if ae.Name != "" && ae.Message != "" {
			return fmt.Sprintf("API request failed (HTTP %d): %s: %s", ae.StatusCode, ae.Name, ae.Message)
		} else if ae.Name != "" {
			return fmt.Sprintf("API request failed (HTTP %d): %s", ae.StatusCode, ae.Name)
		}
		return fmt.Sprintf("API request failed (HTTP %d)", ae.StatusCode)
	}
	return "API request failed"
}

type ErrorBody struct {
	Name    string `json:"error"`
	Message string `json:"message,omitempty"`
}

func (eb *ErrorBody) APIError(statusCode int) error {
	return &APIError{
		StatusCode: statusCode,
		Name:       eb.Name,
		Message:    eb.Message,
	}
}
