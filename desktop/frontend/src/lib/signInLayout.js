// How the sign-in page lays itself out from a server's GET /api/auth/methods
// (design enterprise-identity-and-access §14.7). Kept out of the component so
// the decision is testable without a DOM.
//
// `methods` is null until a server answers, and the page then shows the
// layout every server accepts some account through: the password form first,
// with the browser below it.
//
// password:
//   'first'  — local_login "all": the password form leads, as it always has.
//   'behind' — "system_admins": only operators use it, so it waits behind
//              "Administrator sign-in with a password" and the browser leads.
//   'none'   — "off": the browser is the only way in.
// provider: the SSO provider the browser button names, or '' for the plain
// "Sign in with your browser".
export function signInLayout(methods) {
  const local = methods?.local_login;
  let password = 'first';
  if (local === 'system_admins') password = 'behind';
  if (local === 'off') password = 'none';
  const provider = methods?.oidc?.enabled ? (methods.oidc.display_name ?? '').trim() : '';
  return { password, provider };
}
