package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCounterHandler(t *testing.T) {

	if testing.Short() {
		t.Skip("Skipping test with Redis dependency: short mode enabled")
	}

	initConfig()
	initRedisClient()

	err := rclient.Del(getKey("default")).Err()
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(counterHandler)

	for val := 1; val <= 10; val++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		expected := fmt.Sprintf(`{"value":%d}`, val)
		if rr.Body.String() != expected {
			t.Errorf("handler returned unexpected body: got %v want %v",
				rr.Body.String(), expected)
		}
	}
}

func TestCounterHandlerWithLabel(t *testing.T) {

	if testing.Short() {
		t.Skip("Skipping test with Redis dependency: short mode enabled")
	}

	initConfig()
	initRedisClient()

	err := rclient.Del(getKey("foobar")).Err()
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("GET", "/?label=foobar", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(counterHandler)

	for val := 1; val <= 10; val++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		expected := fmt.Sprintf(`{"value":%d}`, val)
		if rr.Body.String() != expected {
			t.Errorf("handler returned unexpected body: got %v want %v",
				rr.Body.String(), expected)
		}
	}
}

func TestInvalidLabel(t *testing.T) {
	req, err := http.NewRequest("GET", "/?label=%!@#", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(counterHandler)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

}
