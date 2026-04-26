/**
 * TemplatesHelpPane — static help content for the Templates index page.
 *
 * Explains all template-family resource kinds and how they relate.
 * Rendered inside a shadcn Sheet (side="right") toggled by the ? icon
 * in the Templates Card header.
 *
 * The Templates section of the sidebar is a collapsible group with three
 * sub-groups (HOL-1014):
 *   - Policy: Template Policies and Policy Bindings (HOL-1009)
 *   - Dependencies: Template Dependencies and Requirements (HOL-1013)
 *   - Grants: Template Grants (HOL-1013)
 *
 * Keep copy in TSX (not MDX) to avoid any build-system changes (HOL-860).
 */

import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'

export interface TemplatesHelpPaneProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TemplatesHelpPane({ open, onOpenChange }: TemplatesHelpPaneProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="sm:max-w-xl overflow-y-auto">
        <SheetHeader>
          <SheetTitle>Templates — how it works</SheetTitle>
          <SheetDescription>
            Seven resource kinds work together to let platform teams govern
            configuration templates across projects.
          </SheetDescription>
        </SheetHeader>

        <div className="px-4 pb-6 space-y-6 text-sm">
          {/* Template */}
          <section data-testid="help-section-template">
            <h3 className="font-semibold text-base mb-1">Template</h3>
            <p className="text-muted-foreground leading-relaxed">
              A <strong>Template</strong> is a reusable CUE configuration packaged as a
              custom resource. Platform and product authors write templates to express
              standard infrastructure shapes — service meshes, ingress rules, storage
              classes, and more. Templates can be cloned, edited, and scoped at the
              organization, folder, or project level so teams can specialise them
              without modifying the originals.
            </p>
          </section>

          {/* TemplatePolicy */}
          <section data-testid="help-section-template-policy">
            <h3 className="font-semibold text-base mb-1">Template Policy</h3>
            <p className="text-muted-foreground leading-relaxed">
              A <strong>Template Policy</strong> is a constraint defined at organization
              or folder scope — for example, mandating a minimum replica count or
              prohibiting certain image registries. A Template Policy has no effect on
              its own; it must be referenced by a Template Policy Binding before any
              enforcement takes place.
            </p>
          </section>

          {/* TemplatePolicyBinding */}
          <section data-testid="help-section-template-policy-binding">
            <h3 className="font-semibold text-base mb-1">Template Policy Binding</h3>
            <p className="text-muted-foreground leading-relaxed">
              A <strong>Template Policy Binding</strong> attaches a policy to one or more
              templates. It is the enforcement point: without a binding a policy has no
              effect. Bindings are authored by platform engineers, security managers, or
              ISRM teams and can target a single template or a set of templates across
              multiple projects.
            </p>
          </section>

          {/* TemplateDependency */}
          <section data-testid="help-section-template-dependency">
            <h3 className="font-semibold text-base mb-1">Template Dependency</h3>
            <p className="text-muted-foreground leading-relaxed">
              A <strong>Template Dependency</strong> declares that one template requires
              another template to be deployed first. Dependencies are project-scoped and
              express ordering constraints so the platform can resolve a correct deployment
              sequence.
            </p>
          </section>

          {/* TemplateRequirement */}
          <section data-testid="help-section-template-requirement">
            <h3 className="font-semibold text-base mb-1">Template Requirement</h3>
            <p className="text-muted-foreground leading-relaxed">
              A <strong>Template Requirement</strong> records that a template requires a
              specific capability or resource to be present before it can render correctly.
              Requirements are checked at preview and deployment time to surface missing
              prerequisites early.
            </p>
          </section>

          {/* TemplateGrant */}
          <section data-testid="help-section-template-grant">
            <h3 className="font-semibold text-base mb-1">Template Grant</h3>
            <p className="text-muted-foreground leading-relaxed">
              A <strong>Template Grant</strong> gives a project permission to consume a
              template that is owned by a different organization or folder. Without a grant,
              cross-scope template references are denied by the policy resolver.
            </p>
          </section>

          {/* Summary */}
          <section data-testid="help-section-summary">
            <p className="text-muted-foreground leading-relaxed border-t pt-4">
              Authors write templates; platform, SM, and ISRM teams attach policies via
              bindings; dependency and requirement records express ordering and capability
              constraints; grants enable cross-scope sharing; product teams deploy.
            </p>
          </section>
        </div>
      </SheetContent>
    </Sheet>
  )
}
