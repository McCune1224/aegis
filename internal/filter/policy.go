package filter

import (
	"errors"
	"fmt"
	"net/netip"
)

// BlockingMode is how a blocked name is answered. It is policy rather than
// transport, because a profile chooses it and the DNS layer only renders it.
type BlockingMode uint8

const (
	NXDomain BlockingMode = iota
	NullAddress
	CustomAddress
	Refused

	blockingModeCount
)

var blockingModeNames = [blockingModeCount]string{
	NXDomain:      "nxdomain",
	NullAddress:   "null-address",
	CustomAddress: "custom-address",
	Refused:       "refused",
}

func (m BlockingMode) String() string {
	if int(m) >= len(blockingModeNames) {
		return fmt.Sprintf("mode(%d)", uint8(m))
	}
	return blockingModeNames[m]
}

// ParseBlockingMode turns a configured name into a BlockingMode. The names
// table is the single source, so a new mode is one row and both directions keep
// working.
func ParseBlockingMode(name string) (BlockingMode, error) {
	for mode, candidate := range blockingModeNames {
		if candidate == name {
			return BlockingMode(mode), nil
		}
	}
	return 0, fmt.Errorf("filter: unknown blocking mode %q", name)
}

// Policy is what a client is answered with when a rule blocks a name. It
// travels on the verdict so the DNS layer answers without a second lookup.
type Policy struct {
	Mode   BlockingMode
	Custom netip.Addr
}

// Validate refuses a mode paired with an address it cannot use. Compile runs it
// on every resolved profile, so the pairing holds for the life of the rule set
// rather than being rechecked per query.
func (p Policy) Validate() error {
	if int(p.Mode) >= len(blockingModeNames) {
		return fmt.Errorf("filter: unknown blocking mode %d", p.Mode)
	}
	if p.Mode == CustomAddress {
		if !p.Custom.IsValid() {
			return errors.New("filter: blocking mode custom-address needs a valid address")
		}
		return nil
	}
	if p.Custom.IsValid() {
		return fmt.Errorf("filter: blocking mode %s does not take an address", p.Mode)
	}
	return nil
}

// ProfileID names a policy profile.
type ProfileID string

// ClientKey identifies a device once its address has been resolved. An empty
// key means no client was identified, which takes the default profile. Nothing
// can be configured to claim the empty key.
type ClientKey string

// ProfileSpec is a profile as it arrives from config. A nil field means inherit
// the value from the profile this one extends.
type ProfileSpec struct {
	ID      ProfileID
	Extends ProfileID
	Mode    *BlockingMode
	Custom  *netip.Addr
}

// ClientSpec binds one identity to a profile.
type ClientSpec struct {
	Key     ClientKey
	Profile ProfileID
}

// Config is everything Compile needs: the rules, the profiles that choose a
// policy, and the clients that pick a profile.
type Config struct {
	Rules    []RuleSpec
	Profiles []ProfileSpec
	Clients  []ClientSpec
	Default  ProfileID
}

// compileProfiles resolves every profile to a concrete Policy by walking its
// extends chain. Resolution happens here so a query never walks an inheritance
// chain, never checks for a cycle, and never merges a field.
func compileProfiles(specs []ProfileSpec, fallback ProfileID) (map[ProfileID]Policy, error) {
	byID := make(map[ProfileID]ProfileSpec, len(specs))
	for _, spec := range specs {
		if spec.ID == "" {
			return nil, errors.New("filter: a profile has no ID")
		}
		if _, exists := byID[spec.ID]; exists {
			return nil, fmt.Errorf("filter: profile %q is defined twice", spec.ID)
		}
		byID[spec.ID] = spec
	}
	if _, exists := byID[fallback]; !exists {
		return nil, fmt.Errorf("filter: default profile %q is not defined", fallback)
	}

	resolved := make(map[ProfileID]Policy, len(specs))
	visiting := make(map[ProfileID]bool, len(specs))

	var resolve func(id ProfileID) (Policy, error)
	resolve = func(id ProfileID) (Policy, error) {
		if done, exists := resolved[id]; exists {
			return done, nil
		}
		if visiting[id] {
			return Policy{}, fmt.Errorf("filter: profile %q extends itself", id)
		}
		spec, exists := byID[id]
		if !exists {
			return Policy{}, fmt.Errorf("filter: profile %q is not defined", id)
		}

		visiting[id] = true
		defer delete(visiting, id)

		var policy Policy
		if spec.Extends != "" {
			inherited, err := resolve(spec.Extends)
			if err != nil {
				return Policy{}, err
			}
			policy = inherited
		}
		if spec.Mode != nil {
			policy.Mode = *spec.Mode
		}
		if spec.Custom != nil {
			policy.Custom = *spec.Custom
		}
		if err := policy.Validate(); err != nil {
			return Policy{}, fmt.Errorf("filter: profile %q: %w", id, err)
		}

		resolved[id] = policy
		return policy, nil
	}

	for _, spec := range specs {
		if _, err := resolve(spec.ID); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func compileClients(specs []ClientSpec, profiles map[ProfileID]Policy) (map[ClientKey]Policy, error) {
	clients := make(map[ClientKey]Policy, len(specs))
	for _, spec := range specs {
		if spec.Key == "" {
			return nil, errors.New("filter: a client has no key")
		}
		if _, exists := clients[spec.Key]; exists {
			return nil, fmt.Errorf("filter: client %q is defined twice", spec.Key)
		}
		policy, exists := profiles[spec.Profile]
		if !exists {
			return nil, fmt.Errorf("filter: client %q names profile %q, which is not defined", spec.Key, spec.Profile)
		}
		clients[spec.Key] = policy
	}
	return clients, nil
}
