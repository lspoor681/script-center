import { useState, useMemo } from 'react';
import { Param } from '../types';
import './ParamForm.css';

interface ParamFormProps {
  params: Param[];
  onChange: (values: Record<string, any>) => void;
  initialValues?: Record<string, any>;
  expanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
  onBrowse?: (paramName: string) => Promise<void>;
  lastRunValues?: Record<string, any> | null;
  onRepopulate?: () => void;
}

export function ParamForm({
  params,
  onChange,
  initialValues = {},
  expanded = true,
  onToggleExpand,
  onBrowse,
  lastRunValues,
  onRepopulate,
}: ParamFormProps) {
  const [values, setValues] = useState<Record<string, any>>(initialValues);
  const [errors, setErrors] = useState<Record<string, string>>({});

  const requiredParams = useMemo(
    () => params.filter((p) => p.required),
    [params]
  );
  const optionalParams = useMemo(
    () => params.filter((p) => !p.required),
    [params]
  );

  const handleChange = (name: string, value: any) => {
    const newValues = { ...values, [name]: value };
    setValues(newValues);
    onChange(newValues);

    // Clear error on change
    if (errors[name]) {
      setErrors((prev) => {
        const next = { ...prev };
        delete next[name];
        return next;
      });
    }
  };

  const handleArrayChange = (name: string, arr: string[]) => {
    handleChange(name, arr.filter((v) => v.trim() !== ''));
  };

  const validate = (): boolean => {
    const newErrors: Record<string, string> = {};
    for (const param of params) {
      if (param.required && (!values[param.name] || String(values[param.name]).trim() === '')) {
        newErrors[param.name] = 'Required';
      }
      if (param.kind === 'enum' && values[param.name]) {
        const constraint = param.constraints?.find((c) => c.kind === 'set');
        const allowed = constraint?.values;
        if (allowed && !allowed.includes(String(values[param.name]))) {
          newErrors[param.name] = `Must be one of: ${allowed.join(', ')}`;
        }
      }
      if ((param.kind === 'int' || param.kind === 'float') && values[param.name]) {
        const n = Number(values[param.name]);
        if (isNaN(n)) {
          newErrors[param.name] = 'Must be a number';
        } else {
          const range = param.constraints?.find((c) => c.kind === 'range');
          if (range?.hasMin && range.min !== undefined && n < range.min) {
            newErrors[param.name] = `Must be >= ${range.min}`;
          }
          if (range?.hasMax && range.max !== undefined && n > range.max) {
            newErrors[param.name] = `Must be <= ${range.max}`;
          }
        }
      }
    }
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const renderInput = (param: Param) => {
    const error = errors[param.name];
    const value = values[param.name];
    const defaultValue = param.default?.display || param.default?.source || '';
    const isRequired = param.required;

    const label = (
      <>
        <code>{param.name}</code>
        {param.aliases?.length && (
          <span className="param-aliases"> ({param.aliases.join(', ')})</span>
        )}
        {isRequired && <span className="param-required">*</span>}
      </>
    );

    const helpText = [
      param.help,
      param.constraints?.map((c) => c.label).join('; '),
    ]
      .filter(Boolean)
      .join(' — ');

    switch (param.kind) {
      case 'bool':
        return (
          <div className="param-row param-bool">
            <label className="param-label">{label}</label>
            <div className="param-control">
              <input
                type="checkbox"
                checked={value === true}
                onChange={(e) => handleChange(param.name, e.target.checked)}
                disabled={value === undefined && !param.default}
              />
              {param.default && !isRequired && (
                <span className="param-default">Default: {defaultValue}</span>
              )}
            </div>
            {helpText && <div className="param-help">{helpText}</div>}
            {error && <div className="param-error">{error}</div>}
          </div>
        );

      case 'int':
      case 'float':
        return (
          <div className="param-row">
            <label className="param-label">{label}</label>
            <div className="param-control">
              <input
                type="number"
                step={param.kind === 'float' ? 'any' : '1'}
                value={value ?? ''}
                placeholder={defaultValue ? `Default: ${defaultValue}` : ''}
                onChange={(e) =>
                  handleChange(
                    param.name,
                    param.kind === 'int' ? parseInt(e.target.value) || 0 : parseFloat(e.target.value) || 0
                  )
                }
              />
            </div>
            {helpText && <div className="param-help">{helpText}</div>}
            {error && <div className="param-error">{error}</div>}
          </div>
        );

      case 'array':
        return (
          <div className="param-row param-array">
            <label className="param-label">{label}</label>
            <div className="param-control">
              <div className="array-items">
                {(Array.isArray(value) ? value : defaultValue ? [defaultValue] : ['']).map(
                  (item, idx) => (
                    <div key={idx} className="array-item">
                      <input
                        type="text"
                        value={item}
                        onChange={(e) => {
                          const arr = Array.isArray(value) ? [...value] : defaultValue ? [defaultValue] : [''];
                          arr[idx] = e.target.value;
                          handleArrayChange(param.name, arr);
                        }}
                        placeholder={`Value ${idx + 1}`}
                      />
                      {idx > 0 && (
                        <button
                          type="button"
                          className="array-remove"
                          onClick={() => {
                            const arr = Array.isArray(value) ? [...value] : defaultValue ? [defaultValue] : [''];
                            arr.splice(idx, 1);
                            handleArrayChange(param.name, arr);
                          }}
                        >
                          ×
                        </button>
                      )}
                    </div>
                  )
                )}
              </div>
              <button
                type="button"
                className="array-add"
                onClick={() => {
                  const arr = Array.isArray(value) ? [...value] : defaultValue ? [defaultValue] : [''];
                  arr.push('');
                  handleArrayChange(param.name, arr);
                }}
              >
                + Add
              </button>
            </div>
            {helpText && <div className="param-help">{helpText}</div>}
            {error && <div className="param-error">{error}</div>}
          </div>
        );

      case 'enum':
        return (
          <div className="param-row">
            <label className="param-label">{label}</label>
            <div className="param-control">
              <select
                value={value ?? ''}
                onChange={(e) => handleChange(param.name, e.target.value || undefined)}
              >
                <option value="">Select...</option>
                {param.constraints
                  ?.find((c) => c.kind === 'set')
                  ?.values?.map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
              </select>
            </div>
            {helpText && <div className="param-help">{helpText}</div>}
            {error && <div className="param-error">{error}</div>}
          </div>
        );

      case 'path':
        return (
          <div className="param-row param-path">
            <label className="param-label">{label}</label>
            <div className="param-control">
              <input
                type="text"
                value={value ?? ''}
                placeholder={defaultValue ? `Default: ${defaultValue}` : ''}
                onChange={(e) => handleChange(param.name, e.target.value)}
              />
              {onBrowse && (
                <button
                  type="button"
                  className="browse-btn"
                  title="Browse"
                  onClick={() => onBrowse(param.name)}
                >
                  📁
                </button>
              )}
            </div>
            {helpText && <div className="param-help">{helpText}</div>}
            {error && <div className="param-error">{error}</div>}
          </div>
        );

      case 'secret':
        return (
          <div className="param-row param-secret">
            <label className="param-label">{label}</label>
            <div className="param-control">
              <input
                type="password"
                value={value ?? ''}
                placeholder="Enter value (hidden)"
                onChange={(e) => handleChange(param.name, e.target.value)}
                autoComplete="off"
              />
              <span className="secret-note">Will prompt in terminal</span>
            </div>
            {helpText && <div className="param-help">{helpText}</div>}
            {error && <div className="param-error">{error}</div>}
          </div>
        );

      default:
        return (
          <div className="param-row">
            <label className="param-label">{label}</label>
            <div className="param-control">
              <input
                type="text"
                value={value ?? ''}
                placeholder={defaultValue ? `Default: ${defaultValue}` : ''}
                onChange={(e) => handleChange(param.name, e.target.value)}
              />
            </div>
            {helpText && <div className="param-help">{helpText}</div>}
            {error && <div className="param-error">{error}</div>}
          </div>
        );
    }
  };

  const renderParamSection = (title: string, paramList: Param[]) => {
    if (paramList.length === 0) return null;
    return (
      <fieldset className="param-section">
        <legend>{title}</legend>
        {paramList.map((param) => renderInput(param))}
      </fieldset>
    );
  };

  return (
    <div className="param-form">
      {onToggleExpand && (
        <button
          className="param-form-toggle"
          onClick={() => onToggleExpand(!expanded)}
          aria-expanded={expanded}
        >
          <span>{expanded ? '▼' : '▶'} Parameters ({params.length})</span>
          <span className="toggle-actions">
            {lastRunValues && onRepopulate && (
              <button type="button" className="repopulate-btn" onClick={onRepopulate} title="Fill from last run">
                ↺ Repopulate
              </button>
            )}
          </span>
        </button>
      )}

      {expanded && (
        <div className="param-form-content">
          {renderParamSection('Required', requiredParams)}
          {renderParamSection('Optional', optionalParams)}
        </div>
      )}
    </div>
  );
}