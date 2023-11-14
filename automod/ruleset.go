package automod

type RuleSet struct {
	PostRules     []PostRuleFunc
	RecordRules   []RecordRuleFunc
	IdentityRules []IdentityRuleFunc
}

func (r *RuleSet) CallPostRules(evt *PostEvent) error {
	for _, f := range r.PostRules {
		err := f(evt)
		if err != nil {
			return err
		}
		if evt.Err != nil {
			return evt.Err
		}
	}
	return nil
}

func (r *RuleSet) CallRecordRules(evt *RecordEvent) error {
	for _, f := range r.RecordRules {
		err := f(evt)
		if err != nil {
			return err
		}
		if evt.Err != nil {
			return evt.Err
		}
	}
	return nil
}

func (r *RuleSet) CallIdentityRules(evt *IdentityEvent) error {
	for _, f := range r.IdentityRules {
		err := f(evt)
		if err != nil {
			return err
		}
		if evt.Err != nil {
			return evt.Err
		}
	}
	return nil
}
