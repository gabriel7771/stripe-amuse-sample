package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/stripe/stripe-go/v72"
	portalsession "github.com/stripe/stripe-go/v72/billingportal/session"
	"github.com/stripe/stripe-go/v72/checkout/session"
	"github.com/stripe/stripe-go/v72/customer"
	"github.com/stripe/stripe-go/v72/paymentlink"
	"github.com/stripe/stripe-go/v72/price"
	"github.com/stripe/stripe-go/webhook"
)

func main() {
	// This is your test secret API key.
	//stripe.Key = "sk_test_51LgEDEHIBunjXh197gkUy3qZoiQicGQSOQkMrqpmJhAJFYzUDyKkhAADixzpVQzGlxQlslrqE6riS8b81sknugxg00CF0jTWRR"
	if err := godotenv.Load(); err != nil {
		log.Fatalf("godotenv.Load: %v", err)
	}
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")

	http.Handle("/", http.FileServer(http.Dir("public")))
	http.HandleFunc("/create-checkout-session", createCheckoutSession)
	http.HandleFunc("/create-portal-session", createPortalSession)
	http.HandleFunc("/webhook", handleWebhook)
	addr := "localhost:4242"
	log.Printf("Listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func createCheckoutSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	// The lookup_key can only be created through the API
	lookup_key := r.PostFormValue("lookup_key")

	domain := "http://localhost:4242"
	params := &stripe.PriceListParams{
		LookupKeys: stripe.StringSlice([]string{
			lookup_key,
		}),
	}
	i := price.List(params)

	var price *stripe.Price
	for i.Next() {
		p := i.Price()
		price = p
	}
	checkoutParams := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			&stripe.CheckoutSessionLineItemParams{
				Price:    stripe.String(price.ID),
				Quantity: stripe.Int64(1),
			},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			TrialPeriodDays: stripe.Int64(30),
		},
		AllowPromotionCodes: stripe.Bool(true),
		SuccessURL:          stripe.String(domain + "/success.html?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:           stripe.String(domain + "/cancel.html"),
	}

	s, err := session.New(checkoutParams)
	if err != nil {
		log.Printf("session.New: %v", err)
	}

	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}

func createPortalSession(w http.ResponseWriter, r *http.Request) {
	domain := "http://localhost:4242"
	// For demonstration purposes, we're using the Checkout session to retrieve the customer ID.
	// Typically this is stored alongside the authenticated user in your database.
	r.ParseForm()
	sessionId := r.PostFormValue("session_id")
	log.Printf("sessionId: %s", sessionId)
	fmt.Print(sessionId)
	s, err := session.Get(sessionId, nil)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("session.Get: %v", err)
		return
	}

	// Authenticate your user.
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(s.Customer.ID),
		ReturnURL: stripe.String(domain),
	}
	ps, _ := portalsession.New(params)
	log.Printf("ps.New: %v", ps.URL)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("ps.New: %v", err)
		return
	}

	http.Redirect(w, r, ps.URL, http.StatusSeeOther)
}

func handleWebhook(w http.ResponseWriter, req *http.Request) {
	const MaxBodyBytes = int64(65536)
	bodyReader := http.MaxBytesReader(w, req.Body, MaxBodyBytes)
	payload, err := ioutil.ReadAll(bodyReader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading request body: %v\n", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// Replace this endpoint secret with your endpoint's unique secret
	// If you are testing with the CLI, find the secret by running 'stripe listen'
	// If you are using an endpoint defined with the API or dashboard, look in your webhook settings
	// at https://dashboard.stripe.com/webhooks
	//endpointSecret := "whsec_12345"
	endpointSecret := "whsec_a2eada954c764e9b93a819fba7eb6448c4ae3de7c22782e3ff128c828031f251"
	signatureHeader := req.Header.Get("Stripe-Signature")
	event, err := webhook.ConstructEvent(payload, signatureHeader, endpointSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Webhook signature verification failed. %v\n", err)
		w.WriteHeader(http.StatusBadRequest) // Return a 400 error on a bad signature
		return
	}
	// Unmarshal the event data into an appropriate struct depending on its Type
	switch event.Type {
	case "customer.subscription.deleted":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription deleted for %d.", subscription.ID)
		// Then define and call a func to handle the deleted subscription.
		// handleSubscriptionCanceled(subscription)
	case "customer.subscription.updated":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription updated for %d.", subscription.ID)
		// Then define and call a func to handle the successful attachment of a PaymentMethod.
		// handleSubscriptionUpdated(subscription)
	case "customer.subscription.created":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription created for %d.", subscription.ID)
		// Then define and call a func to handle the successful attachment of a PaymentMethod.
		// handleSubscriptionCreated(subscription)
	case "customer.subscription.trial_will_end":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription trial will end for %d.", subscription.ID)
		// Then define and call a func to handle the successful attachment of a PaymentMethod.
		handleSubscriptionTrialWillEnd(subscription)
	default:
		fmt.Fprintf(os.Stderr, "Unhandled event type: %s\n", event.Type)
	}
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("json.NewEncoder.Encode: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := io.Copy(w, &buf); err != nil {
		log.Printf("io.Copy: %v", err)
		return
	}
}

func handleSubscriptionTrialWillEnd(subscription stripe.Subscription) {
	priceId := subscription.Items.Data[0].Price.ID
	params := &stripe.PaymentLinkParams{
		LineItems: []*stripe.PaymentLinkLineItemParams{
			{
				Price:    stripe.String(priceId),
				Quantity: stripe.Int64(1),
			},
		},
	}
	pl, _ := paymentlink.New(params)
	log.Printf("paymentlink.New: %s", pl.URL)
	// This payment link should be sent to the customer
	customerId := subscription.Customer.ID
	customer, _ := customer.Get(customerId, nil)
	log.Printf("Should send payment link to: %s", customer.Email)
}
