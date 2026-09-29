- An artifact a Portal run publishes is now referenced by its Portal page when
  `public_base_url` is set, or by its id alone, instead of a link to the
  internal worker API that no person or later workflow step could open; run
  error messages, such as a disabled Space Secret's, no longer show that
  internal address either.
