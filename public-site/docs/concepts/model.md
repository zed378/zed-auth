---
id: model
title: The object model
description: Instances, organizations, projects, applications and users — how Zed Auth is structured and why.
sidebar_position: 1
---

# The object model

Everything in Zed Auth hangs off six objects. This page explains what each one is and,
more usefully, why the boundaries fall where they do — the shape only makes sense once
you know which problem each division solves.

Four of the six — organizations, projects, applications and users — are creatable and
manageable through the API today, and the [quickstart](/docs/quickstart) walks through
two of them. Roles and grants are the model as designed; they arrive in Phases 2 and 4,
and the sections below say so where they appear.

## The hierarchy

```
Instance
└── Organization
    ├── Project
    │   ├── Application
    │   └── Role
    └── User
```

## Instance

The deployment. One instance is one Zed Auth installation, and it owns the things that
cannot sensibly differ between tenants: the signing keys, the issuer URL, the list of
organizations.

Most people never think about the instance. It matters in exactly one situation — an
instance owner is the only role that can create an organization, and that is the only
operation which crosses a tenant boundary.

## Organization

**The tenant boundary, and the most important line in the system.**

An organization is a company, a department, or any group that manages its own users and
its own access. Two organizations in the same instance cannot see each other's users,
projects, or audit history.

That isolation is enforced by the database rather than by the code that queries it.
Every tenant-scoped table carries a row-level security policy, and the application
connects as a role that cannot bypass it. The practical consequence is that a query
which forgets to filter by organization returns nothing, rather than returning another
tenant's rows — the failure mode is a visible bug instead of a silent data leak.

## Project

A unit of access control, usually corresponding to one product or system.

A project owns two things: the **applications** that authenticate against it, and the
**roles** that mean something within it. Splitting these from the organization is what
lets one organization run several products with genuinely separate permissions — being
an administrator of one project says nothing about the others.

## Application

A client that authenticates users. A web application, a single-page application, a
mobile app, or a machine-to-machine service.

The type matters, because it determines which OAuth flow is allowed and which
protections are mandatory. A single-page application cannot keep a secret, so it uses
Authorization Code with PKCE and no client secret; a backend service can hold one and
may use Client Credentials.

Registering an application is also what establishes its redirect URIs. Those are matched
exactly, never by prefix or pattern — a redirect URI that is matched loosely is how an
authorization code ends up delivered to somebody else's server.

## User

A person, belonging to exactly one organization.

One person, one organization. That constraint is deliberate and is the kind of thing
worth stating plainly, because the alternative looks convenient: let a user belong to
several organizations and switch between them. It brings a question with no good answer
— which organization's password policy, MFA requirement and session lifetime apply
during a session that spans two of them? A user in two organizations is two users who
happen to share an email address.

## Role

A named set of permissions, defined within a project.

Roles are project-scoped rather than global, so `admin` in a billing project and `admin`
in a support project are different roles with different meanings. There is no ambient
"administrator" who can do everything everywhere — except the instance owner, whose
powers are deliberately few.

*Arrives in Phase 2.* The claim namespace a token will carry them in is already
reserved and served empty, so a consumer written today does not change shape when they
appear.

## Grant

The link that gives a user a role.

A **direct grant** gives a user a role in a project belonging to their own organization.

A **project grant** delegates a project to a *different* organization, along with the
specific set of roles that organization may then assign to its own users. This is how a
partner organization manages its own team's access without you administering their
people — and without them being able to grant a role you did not delegate.

The restriction is checked on every request rather than only when the grant is created,
which matters more than it sounds: a delegated set that shrinks later must immediately
constrain grants that already exist, or revocation is advisory.

Both kinds of grant are covered in more detail in [authorization](/docs/concepts/authorization).

*Direct grants arrive in Phase 2, project grants in Phase 4.*

## Why not something simpler

A flat "users and permissions" model is easier to explain and stops working at the first
real requirement — one company, two products, different administrators. The hierarchy
here is the smallest one that survives that, plus the delegation case, without a rewrite
in between.
