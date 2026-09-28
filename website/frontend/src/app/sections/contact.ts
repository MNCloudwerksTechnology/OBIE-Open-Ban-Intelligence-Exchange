import { NgTemplateOutlet } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  Injector,
  afterNextRender,
  inject,
  signal,
} from '@angular/core';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';
import {
  FormControl,
  FormGroup,
  ReactiveFormsModule,
  ValidationErrors,
  ValidatorFn,
  Validators,
} from '@angular/forms';

import { FieldError, Inquiry, InquiryApi, SubmitResult } from '../core/inquiry-api';
import {
  audienceSize,
  futureDate,
  maxTextLength,
  messageLength,
  noControlCharacters,
  requiredText,
  singleLine,
} from '../core/inquiry-validators';
import { InquiryType } from '../content/landing-content.model';
import { LANDING_CONTENT } from '../content/landing.content';

/** Inquiry types that are about an event, so date, location and audience apply. */
export const EVENT_TYPES: readonly InquiryType[] = ['talk', 'workshop', 'interview'];

type State = 'editing' | 'sending' | 'error' | 'success';
type ControlName = keyof ReturnType<typeof inquiryForm>['controls'];
type ErrorKind = Extract<SubmitResult['kind'], 'expired' | 'rate-limited' | 'failed'>;

/**
 * The inquiry form (`/#contact`): fields depend on the inquiry type, mistakes
 * are shown inline before and after sending, and the entries survive every
 * error so the visitor can simply try again.
 */
@Component({
  selector: 'app-contact',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgTemplateOutlet, ReactiveFormsModule],
  templateUrl: './contact.html',
  styleUrl: './contact.scss',
})
export class Contact {
  protected readonly contact = inject(LANDING_CONTENT).contact;
  protected readonly copy = this.contact.form;

  private readonly api = inject(InquiryApi);
  private readonly host: HTMLElement = inject(ElementRef).nativeElement;
  private readonly injector = inject(Injector);

  protected readonly form = inquiryForm();

  protected readonly state = signal<State>('editing');
  protected readonly errorKind = signal<ErrorKind>('failed');
  /** Whether the visitor tried to send; from then on every mistake is shown. */
  protected readonly submitted = signal(false);
  /** Text of the polite live region. */
  protected readonly announcement = signal('');

  protected readonly type = toSignal(this.form.controls.type.valueChanges, {
    initialValue: this.form.controls.type.value,
  });

  constructor() {
    this.form.controls.type.valueChanges
      .pipe(takeUntilDestroyed())
      .subscribe((type) => this.toggleEventFields(type));
    // The form token is a render timestamp: fetch it in the browser only,
    // never while prerendering.
    afterNextRender(() => this.api.prepare());
  }

  protected isEventType(type: InquiryType | null): boolean {
    return type !== null && EVENT_TYPES.includes(type);
  }

  protected control(name: ControlName): FormControl {
    return this.form.controls[name] as FormControl;
  }

  /** The message to show for a field, or null while it is fine or untouched. */
  protected error(name: ControlName): string | null {
    const control = this.form.controls[name];
    if (!control.errors || !(control.touched || this.submitted())) {
      return null;
    }
    return this.message(name, control.errors);
  }

  /** `aria-describedby` of a field: its hint and its error, if shown. */
  protected describedBy(name: ControlName, hint?: string): string | null {
    const ids = [
      hint ? `inquiry-${name}-hint` : null,
      this.error(name) ? `inquiry-${name}-error` : null,
    ].filter((id) => id !== null);
    return ids.length ? ids.join(' ') : null;
  }

  protected async submit(): Promise<void> {
    if (this.state() === 'sending') {
      return;
    }
    this.submitted.set(true);
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      this.showInvalid();
      return;
    }
    this.state.set('sending');
    this.announce(this.copy.sending);
    const result = await this.api.submit(this.inquiry());
    switch (result.kind) {
      case 'accepted':
        this.state.set('success');
        this.announce(this.copy.success.text);
        this.focus('#inquiry-success');
        break;
      case 'invalid':
        this.showServerErrors(result.errors);
        break;
      default:
        this.errorKind.set(result.kind);
        this.state.set('error');
        this.announce('');
    }
  }

  private inquiry(): Inquiry {
    // Disabled controls (the event fields of other types) are not in `value`.
    const value = this.form.value;
    return withoutEmpty({
      type: value.type as InquiryType,
      name: trimmed(value.name),
      email: trimmed(value.email),
      organisation: trimmed(value.organisation),
      eventDate: trimmed(value.eventDate),
      eventLocation: trimmed(value.eventLocation),
      audienceSize: trimmed(value.audienceSize) ? Number(trimmed(value.audienceSize)) : undefined,
      message: trimmed(value.message),
      consent: value.consent === true,
      website: value.website ?? '',
    });
  }

  /** Puts each server message next to its field; unknown fields make it a general error. */
  private showServerErrors(errors: readonly FieldError[]): void {
    const controls = this.form.controls as Record<string, FormControl | undefined>;
    if (errors.some((e) => !controls[e.field] || e.field === 'website')) {
      this.errorKind.set('failed');
      this.state.set('error');
      return;
    }
    for (const { field, message } of errors) {
      controls[field]?.setErrors({ server: message });
      controls[field]?.markAsTouched();
    }
    this.showInvalid();
  }

  private showInvalid(): void {
    this.state.set('editing');
    this.announce(this.copy.invalid);
    this.focus('[aria-invalid="true"]');
  }

  private message(name: ControlName, errors: ValidationErrors): string {
    const m = this.copy.messages;
    const [key] = Object.keys(errors);
    switch (key) {
      case 'server':
        return errors['server'] as string;
      case 'required':
        return {
          type: m.typeRequired,
          name: m.nameRequired,
          email: m.emailRequired,
          message: m.messageRequired,
          consent: m.consentRequired,
        }[name as string] as string;
      case 'email':
        return m.emailInvalid;
      case 'maxLength':
        return m.maxLength.replace('{max}', String((errors[key] as { max: number }).max));
      default:
        // The other validators use the name of their message as error key.
        return m[key as keyof typeof m];
    }
  }

  private toggleEventFields(type: InquiryType | null): void {
    const { eventDate, eventLocation, audienceSize } = this.form.controls;
    for (const control of [eventDate, eventLocation, audienceSize]) {
      if (this.isEventType(type)) {
        control.enable({ emitEvent: false });
      } else {
        control.disable({ emitEvent: false });
      }
    }
  }

  /** Sets the live region; a repeated message gets a trailing space so it is announced again. */
  private announce(text: string): void {
    this.announcement.set(this.announcement() === text ? `${text}\u00a0` : text);
  }

  private focus(selector: string): void {
    afterNextRender(() => this.host.querySelector<HTMLElement>(selector)?.focus(), {
      injector: this.injector,
    });
  }
}

function inquiryForm() {
  return new FormGroup({
    type: new FormControl<InquiryType | null>('talk', Validators.required),
    name: text(requiredText, maxTextLength(200), singleLine),
    email: text(requiredText, Validators.email, maxTextLength(254)),
    organisation: text(maxTextLength(200), singleLine),
    eventDate: text(futureDate),
    eventLocation: text(maxTextLength(200), singleLine),
    audienceSize: text(audienceSize),
    message: text(requiredText, messageLength, noControlCharacters),
    consent: new FormControl(false, { nonNullable: true, validators: Validators.requiredTrue }),
    /** Honeypot: hidden from people. */
    website: text(),
  });
}

function text(...validators: ValidatorFn[]): FormControl<string> {
  return new FormControl('', { nonNullable: true, validators });
}

function trimmed(value: string | undefined): string {
  return (value ?? '').trim();
}

/** The inquiry without empty optional fields, which the back end expects left out. */
function withoutEmpty(inquiry: Inquiry): Inquiry {
  return Object.fromEntries(
    Object.entries(inquiry).filter(
      ([key, value]) => value !== undefined && (value !== '' || key === 'website'),
    ),
  ) as unknown as Inquiry;
}
