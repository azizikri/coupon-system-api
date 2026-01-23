package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/azizikri/flash-sale-coupon/internal/domain"
	"github.com/azizikri/flash-sale-coupon/internal/usecase"
	"github.com/go-chi/chi/v5"
)

type CreateCouponRequest struct {
	Name   string `json:"name"`
	Amount int    `json:"amount"`
}

type ClaimRequest struct {
	UserID     string `json:"user_id"`
	CouponName string `json:"coupon_name"`
}

type CouponResponse struct {
	Name            string   `json:"name"`
	Amount          int      `json:"amount"`
	RemainingAmount int      `json:"remaining_amount"`
	ClaimedBy       []string `json:"claimed_by"`
}

type Handler struct {
	service *usecase.CouponService
}

func NewHandler(service *usecase.CouponService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		r.Post("/coupons", h.CreateCoupon)
		r.Post("/coupons/claim", h.ClaimCoupon)
		r.Get("/coupons/{name}", h.GetCouponDetails)
	})
}

func (h *Handler) CreateCoupon(w http.ResponseWriter, r *http.Request) {
	var req CreateCouponRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := h.service.CreateCoupon(r.Context(), req.Name, req.Amount)
	if err != nil {
		if errors.Is(err, domain.ErrDuplicateCoupon) {
			http.Error(w, "coupon already exists", http.StatusConflict)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) ClaimCoupon(w http.ResponseWriter, r *http.Request) {
	var req ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := h.service.ClaimCoupon(r.Context(), req.UserID, req.CouponName)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "coupon not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, domain.ErrAlreadyClaimed) {
			http.Error(w, "already claimed", http.StatusConflict)
			return
		}
		if errors.Is(err, domain.ErrSoldOut) {
			http.Error(w, "sold out", http.StatusConflict)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GetCouponDetails(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	coupon, err := h.service.GetCouponDetails(r.Context(), name)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "coupon not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := CouponResponse{
		Name:            coupon.Name,
		Amount:          coupon.Amount,
		RemainingAmount: coupon.RemainingAmount,
		ClaimedBy:       coupon.ClaimedBy,
	}

	if resp.ClaimedBy == nil {
		resp.ClaimedBy = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
