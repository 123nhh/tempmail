import test from 'node:test'
import assert from 'node:assert/strict'
import { extractOtp } from '../src/utils/otp.js'

test('extractOtp ignores substrings like Shipping (PIN) and hotpot (OTP)', () => {
  assert.equal(
    extractOtp({
      subject: 'Shipping update',
      text: 'Your Shipping order #AB1234 is on the way, enjoy hotpot tonight!',
    }),
    null,
  )
})

test('extractOtp matches valid Chinese and English keywords', () => {
  assert.equal(extractOtp({ subject: '登录验证码：842910' }), '842910')
  assert.equal(extractOtp({ text: 'Your verification code is 482915. Valid for 10 min.' }), '482915')
  assert.equal(extractOtp({ text: 'One-time passcode: A9B8C7' }), 'A9B8C7')
  assert.equal(extractOtp({ text: 'Your PIN is 123-456' }), '123456')
  assert.equal(extractOtp({ text: 'Security OTP: 7391' }), '7391')
})

test('extractOtp does not falsely match years or all-zero numbers', () => {
  assert.equal(extractOtp({ subject: 'Newsletter 2026', text: 'Copyright 2026 0000' }), null)
})
