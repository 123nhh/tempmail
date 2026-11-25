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

test('extractOtp reads codes after 为：/是：/is: even when an order number comes first', () => {
  assert.equal(extractOtp({ text: '订单号 20481937，您的验证码为：839201' }), '839201')
  assert.equal(extractOtp({ text: '订单 55667788 验证码是：839201' }), '839201')
  assert.equal(extractOtp({ text: 'Order 55667788 Your one-time code is: 839201' }), '839201')
  assert.equal(extractOtp({ text: '您的验证码为：AB12CD' }), 'AB12CD')
  assert.equal(extractOtp({ text: '您的验证码为 839201' }), '839201')
  assert.equal(extractOtp({ text: 'Your code is - 4821' }), '4821')
})
