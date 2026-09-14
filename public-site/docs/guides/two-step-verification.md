---
id: two-step-verification
title: Set up two-step verification, and get back in without your phone
description: For people signing in — adding an authenticator app or a passkey, keeping recovery codes, and what to do when a device is lost.
sidebar_position: 5
---

# Set up two-step verification, and get back in without your phone

This page is for **people who sign in** to an application that uses Zed Auth. If you
build or run one, the [step-up guide](/docs/guides/step-up-with-amr) and the
[administrator guide](/docs/guides/require-mfa) are the ones for you.

Two-step verification means a stolen password is not enough on its own. After your
password, you prove you hold something else: a code from an app on your phone, or a
passkey.

## Where to set it up

Open **Your account** in the console — the link is at the top of every page once you are
signed in. Your account works on a phone as well as a computer, so you can set up the app
on the device that runs it.

The **Multi-factor authentication** section lists what you have and offers what you can
add. If it says second factors are not available, the service you are using has not
turned them on. Ask the people who run it.

:::note[Changing how you sign in asks for a recent sign-in]

Adding or removing a factor and generating recovery codes need a sign-in from **the last
10 minutes**. If yours is older, the page asks you to sign in again and brings you back.
It is not an error: somebody who found your computer unlocked should not be able to
change how your account is protected.

:::

## Option 1: an authenticator app

Any app that shows six-digit codes that change every 30 seconds works. Google
Authenticator, Microsoft Authenticator, 1Password and Bitwarden are all examples.

1. Choose **Add authenticator app**.
2. Scan the QR code with the app. If you cannot scan it, type the key shown under it
   into the app instead.
3. Enter the six-digit code the app now shows, to prove the two are connected.

Nothing is switched on until step 3 succeeds. If you give up halfway, your account is
exactly as it was.

## Option 2: a passkey or security key

A passkey is kept by your device and unlocked with your fingerprint, face, PIN or a
hardware key. Nobody can read it to you over the phone, so a fake sign-in page cannot
talk you into giving it away.

1. Choose **Add passkey**. You are taken to a Zed Auth page to register it. That is
   deliberate: a passkey is bound to the website it was made for.
2. Follow your browser's prompt.
3. You come back to your account.

A passkey here is a **second** step after your password, not a replacement for it.

## Recovery codes: do not skip this part

When you add a factor and have no unused recovery codes, you get **ten recovery codes**.
They are shown once. Each looks like `ABCD-EFGH-JK23-MN45` and works once.

They are how you get back in if you lose your phone. Keep them somewhere that is not the
phone: a password manager, or printed and put away.

- **Case, dashes and spaces do not matter** when you type one. The codes never contain
  the digits `0`, `1`, `8` or `9`. If you read `0` where the code has `O`, it is
  accepted as `O`.
- **Your account shows how many are left**, and warns you when **three or fewer**
  remain.
- **Generate a new set** from your account whenever you like. The new set replaces every
  old code, used or not.

If you add another factor while you still have unused codes, you keep those codes and
are not given more.

## Signing in

After your password, you are asked for your second step. If you have both an app and a
passkey, you choose which one to use.

A code that is refused is usually a clock problem, not a broken app. Codes are accepted
for **30 seconds either side** of the current time. A phone whose clock has drifted
further than that produces correct codes that are refused. Turn on automatic date and
time on the phone and try again.

After **ten wrong codes in fifteen minutes**, every code is refused for a while,
including correct ones. Wait, then try again. Your account is not locked, and nobody
can unlock it sooner.

## You lost your phone

**If you have your recovery codes:**

1. Sign in with your password as usual.
2. On the second step, type any unused code into **Recovery code** and choose **Use a
   recovery code**.
3. Once you are in, go to **Your account** and:
   - **remove the lost factor**, so the lost phone cannot be used to sign in;
   - **add a new one** on your new device;
   - **generate new recovery codes**, because you just used one;
   - under **Where you are signed in**, **revoke** any session on the lost device, or
     choose **Revoke all other sessions**.

If your organization requires a second factor, you cannot remove your only one. Add the
new one first, then remove the old one.

**If you do not have your recovery codes**, you cannot get back in by yourself. That is
deliberate. Contact your organization's administrator.

They can remove your factors so that your password alone works again, and then you set
up a new one. Before they do, **expect them to check who you are**, for example with a
video call or by calling a number they already have for you. They will not send you a
code or a new password. The reset only removes your factors; it gives nobody a way in
except through your own password.

## If your organization requires it

Your organization can require a second factor for everyone. When it does:

- **If you already have one**, nothing changes.
- **If you do not**, **Your account** tells you the date by which to add one. Until then
  you sign in normally.
- **After that date**, signing in takes you straight to setting up an authenticator app,
  before you can continue. You are given your recovery codes on the same step.

## A sign-in you did not make

If the service sends sign-in notices, a notice about a sign-in you do not recognize has a
**This wasn't me** link. It signs you out everywhere, stops your current password
working, and emails you a link to choose a new one. Whether notices are sent is up to the
people who run the service.

Changing your password from **Your account** also signs you out of every other device.
