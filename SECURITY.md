# Security Process and Policy
This document outlines the kube-burner security policy and describes the processes for managing security incidents, including a step-by-step guide for reporting vulnerabilities within the kube-burner organization.

  - [Report A Vulnerability](#report-a-vulnerability)
    - [When To Send A Report](#when-to-send-a-report)
    - [Security Vulnerability Response](#security-vulnerability-response)
    - [Public Disclosure](#public-disclosure)
  - [Security Team Membership](#security-team-membership)
    - [Responsibilities](#responsibilities)
    - [Membership](#membership)
  - [Patch and Release Team](#patch-and-release-team)
  - [Disclosures](#disclosures)

## Report A Vulnerability

We sincerely appreciate security researchers and users who report vulnerabilities to the kube-burner community. Every report is carefully reviewed by a team of kube-burner maintainers.

Easiest way to report a vulnerability is through [Security Tab On Github](https://github.com/kube-burner/kube-burner/security/advisories). This mechanism allows maintainers to communicate privately with you, and you do not need to encrypt your messages. 

Alternatively you can also email the private security list at cncf-kube-burner-maintainers@lists.cncf.io
 with the details. Please do not discuss potential vulnerabilities in public without validating with us first.

For any other concerns/questions come talk to us at our [slack-channel](https://kubernetes.slack.com/archives/C06EEGHTNJ1) in the kubernetes slack server.

The list of people on the security list is below: 

| Name            | GitHub Handle    |
| --------------- | ---------------- |
| Raul Sevilla   | @rsevilla87  |
| Vishnu Challa | @vishnuchalla |
| Ygal Blum | @ygalblum |
| Sai Sindhur Malleni   | @smalleni |


### When To Send A Report

If you believe you've discovered a vulnerability within kube-burner project or one of its dependencies, it may involve any repository within the [kube-burner github organization](https://github.com/kube-burner).


### Security Vulnerability Response

Each report will be reviewed, and acknowledgment of receipt will be provided within 5 business days, initiating the security review process outlined below.

Any vulnerability information shared with the security team will remain within the kube-burner project and will only be disclosed to others if necessary to resolve the issue. Information is shared strictly on a need-to-know basis.

We request that reporters act in good faith by refraining from disclosing the issue to others. In return, we commit to responding promptly and ensuring that reporters receive proper credit for their contributions.

Throughout the triage, investigation, and resolution process, the reporter will be kept informed of progress. If needed, the security team may also reach out for additional details regarding the vulnerability.

### Public Disclosure

Security vulnerabilities are publicly disclosed alongside release updates or details on the fix. We aim to fully disclose vulnerabilities once a mitigation strategy is available, ensuring a timely release that balances speed with user needs.

All identified vulnerabilities will be assigned a CVE. However, due to the time required to obtain a CVE ID, disclosures may be published before an ID is assigned. In such cases, the disclosure will be updated once the CVE ID becomes available.

If the reporter wishes to be acknowledged in the disclosure, we are happy to do so. We will seek permission and confirm how they would like to be credited. We greatly appreciate vulnerability reports and are committed to recognizing contributors who choose to be acknowledged.

## Security Team Membership

The security team consists of select kube-burner project maintainers who are equipped and available to handle vulnerability reports.

### Responsibilities

* Members MUST be active maintainers of currently supported kube-burner projects.
* Members SHOULD participate in each reported vulnerability, at minimum ensuring it is being properly addressed.
* Members MUST keep vulnerability details confidential and share them only on a need-to-know basis.

### Membership

New members are required to be active maintainers of kube-burner projects who are willing to perform
the responsibilities outlined above. The security team is a subset of the maintainers. Members can
step down at any time and may join at any time.

## Patch and Release Team

When a vulnerability is reported and acknowledged, a team—including maintainers of the affected kube-burner project will be assembled to develop a patch, release an update, and publish a disclosure. If necessary, additional maintainers may be involved, but participation will remain within the kube-burner project maintainer group.

## Disclosures

Security advisories for vulnerabilities are published in the relevant project repositories. These advisories include an overview, details of the vulnerability, a fix—typically provided through an update—and, if available, a workaround.

Disclosures are released on the same day as the fix, following the publication of the update. The release notes will also include a link to the advisory.