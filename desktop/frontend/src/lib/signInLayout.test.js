import { describe, expect, it } from 'vitest';
import { signInLayout } from './signInLayout';

describe('signInLayout', () => {
  it('leads with the password form until a server says otherwise', () => {
    expect(signInLayout(null)).toEqual({ password: 'first', provider: '' });
  });

  it('follows local_login', () => {
    expect(signInLayout({ local_login: 'all', oidc: { enabled: false } }).password).toBe('first');
    expect(signInLayout({ local_login: 'system_admins', oidc: { enabled: false } }).password).toBe('behind');
    expect(signInLayout({ local_login: 'off', oidc: { enabled: false } }).password).toBe('none');
  });

  it('names the provider only when SSO is on and has a name', () => {
    expect(signInLayout({ local_login: 'off', oidc: { enabled: true, display_name: ' Okta ' } }).provider).toBe('Okta');
    expect(signInLayout({ local_login: 'off', oidc: { enabled: true } }).provider).toBe('');
    expect(signInLayout({ local_login: 'all', oidc: { enabled: false, display_name: 'Okta' } }).provider).toBe('');
  });
});
