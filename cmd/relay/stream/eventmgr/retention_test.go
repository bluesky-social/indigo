package eventmgr

import "testing"

func TestRemoveSubscriberReleasesBackingReference(t *testing.T) {
	for _, index := range []int{0, 1, 2} {
		t.Run([]string{"first", "middle", "last"}[index], func(t *testing.T) {
			subs := []*Subscriber{{ident: "first"}, {ident: "middle"}, {ident: "last"}}
			backing := append([]*Subscriber(nil), subs...)
			em := &EventManager{subs: backing}
			em.rmSubscriber(subs[index])

			if len(em.subs) != 2 || backing[2] != nil {
				t.Fatalf("removed subscriber remains in backing slice: %+v", backing)
			}
			for i, sub := range subs {
				if i == index {
					continue
				}
				found := false
				for _, active := range em.subs {
					found = found || active == sub
				}
				if !found {
					t.Fatalf("active subscriber %q was removed", sub.ident)
				}
			}

			replacement := &Subscriber{ident: "replacement"}
			em.addSubscriber(replacement)
			if &em.subs[0] != &backing[0] || backing[2] != replacement {
				t.Fatal("adding subscriber did not reuse the cleared backing slot")
			}
			for i, sub := range subs {
				if i != index {
					em.rmSubscriber(sub)
				}
			}
			em.rmSubscriber(replacement)
			for _, sub := range backing {
				if sub != nil {
					t.Fatal("empty subscriber list retains a subscriber")
				}
			}
		})
	}
}
