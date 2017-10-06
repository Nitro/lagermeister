import { Component } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'lag-gauge',
    templateUrl: './lag-gauge.component.html',
    styleUrls: ['./lag-gauge.component.scss'],

    providers: [ChartsService],
})

export class LagGaugeComponent {

    gaugeObject: any;

    constructor(private chartsService: ChartsService) {
        this.gaugeObject = {
            chartType: 'Gauge',
            data: {},
            chart: null,
            domElement: 'lag-gauge',
            chartReady: false,
            columns: [['Label', 'string'], ['Lag (secs)', 'number']],
            options: {
                width: 200,
                height: 200,
                chartArea: {top: 30, left: 0, right: 100},
            }
        };

        let script = document.createElement('script');
        script.src = '//www.google.com/jsapi';
        script.onload = () => {
            (<any>window).google.charts.load("visualization", "1", {packages:["corechart", "gauge"]});
            (<any>window).google.charts.setOnLoadCallback(this.chartsService.drawGauge.bind(this, this.gaugeObject));
        };
        document.head.appendChild(script);

        let pendingData: Array<Object> = [];

        let obsv = this.chartsService.getObservable();
        obsv.subscribe( (evt: any)  => {
            let data = JSON.parse(evt.data);
            if (data.MetricType === 'Lag') {
                pendingData.push(data);
            }
        });

        setInterval(() => {
            _.forEach(pendingData, (data: any) => {
                if(this.gaugeObject.chartReady) {
                    this.chartsService.updateGauge(data, this.gaugeObject);
                }
            });
            pendingData.splice(0);
        }, 1000)

    }
}
